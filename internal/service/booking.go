package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"clientportal/internal/model"
)

// BookingService lets a customer pick a day and time and book a service. Booking creates an
// open estimate, so the estimate and invoice flow takes over from there.
type BookingService struct {
	docs     BillingDocumentRepository
	locs     LocationRepository
	services ServiceRepository
	hours    model.Hours
	maxLive  int // upcoming live bookings one customer may hold
	now      func() time.Time
}

// NewBookingService returns a booking service; a customer may hold at most maxLive upcoming bookings.
func NewBookingService(docs BillingDocumentRepository, locs LocationRepository, services ServiceRepository,
	hours model.Hours, maxLive int, now func() time.Time) *BookingService {
	return &BookingService{docs: docs, locs: locs, services: services, hours: hours, maxLive: maxLive, now: now}
}

// Location is the business timezone; responses show booked times in it.
func (s *BookingService) Location() *time.Location { return s.hours.Loc }

// Days returns a page of up to seven bookable dates from `from` ("" means tomorrow).
func (s *BookingService) Days(from string) (days []time.Time, prev, next *time.Time, err error) {
	now := s.now()
	start, _ := s.hours.BookableRange(now)
	if from != "" {
		if start, err = model.ParseDate(from); err != nil {
			return nil, nil, nil, err
		}
	}
	return s.hours.Days(now, start)
}

// bookable returns the service when customers can book it; an archived one is not found.
func (s *BookingService) bookable(ctx context.Context, id int64) (model.Service, error) {
	svc, err := s.services.Get(ctx, id)
	if err != nil {
		return model.Service{}, err
	}
	if svc.Status != model.ServiceActive {
		return model.Service{}, fmt.Errorf("service %d: %w", id, model.ErrNotFound)
	}
	return svc, nil
}

// Slots returns the free start times for a service on a date.
func (s *BookingService) Slots(ctx context.Context, serviceID int64, date string) (time.Time, []string, error) {
	day, err := model.ParseDate(date)
	if err != nil {
		return time.Time{}, nil, err
	}
	if err := s.hours.CheckBookable(s.now(), day); err != nil {
		return time.Time{}, nil, err
	}
	svc, err := s.bookable(ctx, serviceID)
	if err != nil {
		return time.Time{}, nil, fmt.Errorf("slots: %w", err)
	}
	from, to := s.hours.DayBounds(day)
	busy, err := s.docs.BusyIntervals(ctx, from, to)
	if err != nil {
		return time.Time{}, nil, fmt.Errorf("slots: %w", err)
	}
	return day, s.hours.FreeSlots(day, svc.DurationMinutes, busy), nil
}

// Book creates the open estimate and the booking in one transaction. A slot taken meanwhile
// fails the database's no-overlap constraint and comes back as ErrConflict.
func (s *BookingService) Book(ctx context.Context, customerID int64, in model.BookingInput) (model.Booking, error) {
	svc, err := s.bookable(ctx, in.ServiceID)
	if err != nil {
		return model.Booking{}, fmt.Errorf("book: %w", err)
	}
	if svc.Recurrence.Repeats() {
		return model.Booking{}, fmt.Errorf("%w: service %d repeats; book it with /booking-series", model.ErrValidation, svc.ID)
	}
	start, err := s.hours.ParseSlot(s.now(), in.Date, in.Time, svc.DurationMinutes)
	if err != nil {
		return model.Booking{}, err
	}
	if _, err := s.locs.Get(ctx, customerID, in.LocationID); err != nil {
		return model.Booking{}, fmt.Errorf("book: location: %w", err)
	}
	day, err := model.ParseDate(in.Date)
	if err != nil {
		return model.Booking{}, err
	}
	// shortcut: concurrent requests from one customer can pass this check together and exceed the
	// cap by the number in flight; lock the customer row here if that ever matters.
	live, err := s.docs.ListUpcoming(ctx, customerID, s.now())
	if err != nil {
		return model.Booking{}, fmt.Errorf("book: %w", err)
	}
	// Series jobs do not count: a series is its own unit, limited by the booking horizon.
	single := 0
	for _, b := range live {
		if b.SeriesID == nil {
			single++
		}
	}
	if single >= s.maxLive {
		return model.Booking{}, fmt.Errorf("%w: you already have %d upcoming bookings; cancel one first", model.ErrConflict, single)
	}

	var id int64
	err = s.docs.WithTx(ctx, func(tx BillingDocumentRepository) error {
		var err error
		id, err = s.createJob(ctx, tx, svc, customerID, in.LocationID, day, start, nil)
		return err
	})
	if err != nil {
		return model.Booking{}, fmt.Errorf("book: %w", err)
	}
	return s.Get(ctx, customerID, id)
}

// createJob writes one estimate and the booking that points at it, copying the template's items.
func (s *BookingService) createJob(ctx context.Context, tx BillingDocumentRepository, svc model.Service,
	customerID, locationID int64, day, start time.Time, seriesID *int64) (int64, error) {
	items := svc.EstimateItems()
	// The customer's confirmation is the request, so the estimate starts open unless the template says draft.
	status := model.StatusOpen
	if !svc.SetConfirmed {
		status = model.StatusDraft
	}
	estimateID, err := tx.Create(ctx, model.BillingDocument{
		ParentType:   model.ParentEstimate,
		CustomerID:   customerID,
		Location:     model.Location{ID: locationID},
		Title:        svc.Name,
		Status:       status,
		DocumentDate: day,
		TotalCents:   model.SumItems(items),
		Items:        items,
	})
	if err != nil {
		return 0, err
	}
	return tx.CreateBooking(ctx, model.Booking{
		CustomerID: customerID,
		Service:    model.Service{ID: svc.ID},
		Location:   model.Location{ID: locationID},
		Estimate:   model.BillingDocument{ID: estimateID},
		StartsAt:   start,
		EndsAt:     start.Add(time.Duration(svc.DurationMinutes) * time.Minute),
		SeriesID:   seriesID,
	})
}

// BookSeries books a recurring template: it records the series and returns its id. No job exists
// yet; GenerateDue places each one, with its estimate, shortly before its date.
func (s *BookingService) BookSeries(ctx context.Context, customerID int64, in model.BookingInput) (int64, error) {
	svc, err := s.bookable(ctx, in.ServiceID)
	if err != nil {
		return 0, fmt.Errorf("book series: %w", err)
	}
	if !svc.Recurrence.Repeats() {
		return 0, fmt.Errorf("%w: service %d does not repeat; book it with /bookings", model.ErrValidation, svc.ID)
	}
	first, err := s.hours.ParseSlot(s.now(), in.Date, in.Time, svc.DurationMinutes)
	if err != nil {
		return 0, err
	}
	if _, err := s.locs.Get(ctx, customerID, in.LocationID); err != nil {
		return 0, fmt.Errorf("book series: location: %w", err)
	}
	live, err := s.docs.HasLiveSeries(ctx, customerID, svc.ID)
	if err != nil {
		return 0, fmt.Errorf("book series: %w", err)
	}
	if live {
		return 0, fmt.Errorf("%w: you already have a running series of service %d", model.ErrConflict, svc.ID)
	}
	firstDay := s.hours.LocalDay(first)
	if len(svc.Recurrence.Days(firstDay, firstDay)) == 0 { // the series ended before the chosen first date
		return 0, fmt.Errorf("%w: the series ends before %s", model.ErrValidation, in.Date)
	}
	id, err := s.docs.CreateSeries(ctx, customerID, in.LocationID, svc.ID, first, firstDay.AddDate(0, 0, -1))
	if err != nil {
		return 0, fmt.Errorf("book series: %w", err)
	}
	return id, nil
}

// EndSeries stops one of the customer's series from placing more jobs. Jobs already placed stay;
// cancel them one by one.
func (s *BookingService) EndSeries(ctx context.Context, customerID, id int64) error {
	if err := s.docs.EndSeries(ctx, customerID, id); err != nil {
		return fmt.Errorf("end series: %w", err)
	}
	return nil
}

// GenerateDue places the jobs of every running series whose date falls within the lead time: for
// each, an estimate and a booking with its work order, the same as a single booking. One series
// failing does not hold back the others.
func (s *BookingService) GenerateDue(ctx context.Context) error {
	now := s.now()
	through := s.hours.DueThrough(now)
	ids, err := s.docs.ListDueSeries(ctx, through)
	if err != nil {
		return fmt.Errorf("generate jobs: %w", err)
	}
	var errs []error
	for _, id := range ids {
		if err := s.generate(ctx, id, now, through); err != nil {
			errs = append(errs, fmt.Errorf("series %d: %w", id, err))
		}
	}
	return errors.Join(errs...)
}

// generate places one series' due jobs in one transaction and moves its cursor to `through`.
// A date whose slot another booking holds, or whose time has already passed, is skipped and logged.
// shortcut: Days recounts from the first date on every run, fine for years of daily jobs; keep the
// next date on the series if that ever shows up in the profile.
func (s *BookingService) generate(ctx context.Context, id int64, now, through time.Time) error {
	return s.docs.WithTx(ctx, func(tx BillingDocumentRepository) error {
		run, err := tx.LockSeries(ctx, id)
		if errors.Is(err, model.ErrNotFound) { // another instance has it, or it ended meanwhile
			return nil
		}
		if err != nil {
			return err
		}
		svc, err := s.services.Get(ctx, run.ServiceID)
		if err != nil {
			return err
		}
		rule := svc.Recurrence
		firstDay := s.hours.LocalDay(run.FirstStart)
		var due []time.Time
		if svc.Status != model.ServiceDeleted {
			for _, d := range rule.Days(firstDay, through) {
				if d.After(run.GeneratedThrough) {
					due = append(due, d)
				}
			}
		}
		if len(due) > 0 {
			from, _ := s.hours.DayBounds(due[0])
			_, to := s.hours.DayBounds(due[len(due)-1])
			busy, err := tx.BusyIntervals(ctx, from, to)
			if err != nil {
				return err
			}
			length := time.Duration(svc.DurationMinutes) * time.Minute
			for _, d := range due {
				start := s.hours.SameTime(d, run.FirstStart)
				taken := slices.ContainsFunc(busy, func(iv model.Interval) bool {
					return iv.Start.Before(start.Add(length)) && start.Before(iv.End)
				})
				if taken || !start.After(now) {
					slog.WarnContext(ctx, "recurring job skipped", "series", id, "date", d.Format(time.DateOnly), "slot_taken", taken)
					continue
				}
				if _, err := s.createJob(ctx, tx, svc, run.CustomerID, run.LocationID, d, start, &id); err != nil {
					return err
				}
			}
		}
		// A deleted template, or a rule with an end that the cursor has reached, ends the series.
		done := svc.Status == model.ServiceDeleted
		if rule.Repeats() && rule.EndsKind != model.EndsNever {
			all := rule.Days(firstDay, time.Date(9999, 12, 31, 0, 0, 0, 0, time.UTC))
			done = done || len(all) == 0 || !all[len(all)-1].After(through)
		}
		return tx.AdvanceSeries(ctx, id, through, done)
	})
}

// Cancel cancels one of the customer's live bookings and declines its estimate, freeing the slot.
// It works while the estimate is draft, open or accepted, but not once it became an invoice or staff
// started the work. The status is set directly: the public /decline transition keeps `accepted` terminal.
func (s *BookingService) Cancel(ctx context.Context, customerID, id int64) (model.Booking, error) {
	err := s.docs.WithTx(ctx, func(tx BillingDocumentRepository) error {
		b, err := tx.GetBooking(ctx, customerID, id)
		if err != nil {
			return err
		}
		if b.CancelledAt != nil {
			return fmt.Errorf("%w: booking %d is already cancelled", model.ErrConflict, id)
		}
		if b.Work != model.WorkScheduled {
			return fmt.Errorf("%w: work on booking %d has already started", model.ErrConflict, id)
		}
		// A visit that took place must stay billable: cancelling would decline its estimate for good.
		if !b.StartsAt.After(s.now()) {
			return fmt.Errorf("%w: booking %d has already started", model.ErrConflict, id)
		}
		// Lock the estimate first: Convert holds the same lock, so it cannot slip in after the check below.
		est, err := tx.GetForUpdate(ctx, customerID, model.ParentEstimate, b.Estimate.ID)
		if err != nil {
			return err
		}
		if est.Status != model.StatusDraft && est.Status != model.StatusOpen && est.Status != model.StatusAccepted {
			return fmt.Errorf("%w: estimate %d is %s and cannot be cancelled", model.ErrConflict, est.Number, est.Status)
		}
		converted, err := tx.HasConvertedInvoice(ctx, est.ID)
		if err != nil {
			return err
		}
		if converted {
			return fmt.Errorf("%w: estimate %d already became an invoice", model.ErrConflict, est.Number)
		}
		if err := tx.UpdateStatus(ctx, est.ID, model.StatusDeclined); err != nil {
			return err
		}
		return tx.CancelBookingForEstimate(ctx, est.ID)
	})
	if err != nil {
		return model.Booking{}, fmt.Errorf("cancel booking: %w", err)
	}
	return s.Get(ctx, customerID, id)
}

// ListWork returns one page of the customer's work orders and the id to continue after, 0 on the
// last page.
func (s *BookingService) ListWork(ctx context.Context, f model.WorkFilter) (items []model.Booking, nextAfterID int64, err error) {
	if f.Limit < 1 {
		return nil, 0, fmt.Errorf("%w: limit must be at least 1", model.ErrValidation)
	}
	if f.Status != "" && !model.ValidWorkStatus(f.Status) {
		return nil, 0, fmt.Errorf("%w: unknown work order status %q", model.ErrValidation, f.Status)
	}
	limit := f.Limit
	f.Limit++
	items, err = s.docs.ListWorkOrders(ctx, f)
	if err != nil {
		return nil, 0, fmt.Errorf("list work orders: %w", err)
	}
	if len(items) > limit {
		items = items[:limit]
		nextAfterID = items[limit-1].WorkOrderID
	}
	return items, nextAfterID, nil
}

// Start marks a scheduled booking's work order in progress.
func (s *BookingService) Start(ctx context.Context, id int64) error {
	if err := s.docs.AdvanceWork(ctx, id, model.WorkScheduled, model.WorkInProgress, nil); err != nil {
		return fmt.Errorf("start work: %w", err)
	}
	return nil
}

// Complete marks an in-progress work order completed, saving the notes when given. The job of a
// recurring series is then billed: its estimate is accepted and turned into a draft invoice, unless
// the customer already converted it or declined it.
func (s *BookingService) Complete(ctx context.Context, id int64, notes *string) error {
	err := s.docs.WithTx(ctx, func(tx BillingDocumentRepository) error {
		if err := tx.AdvanceWork(ctx, id, model.WorkInProgress, model.WorkCompleted, notes); err != nil {
			return err
		}
		customerID, estimateID, ok, err := tx.SeriesJobEstimate(ctx, id)
		if err != nil || !ok {
			return err
		}
		est, err := tx.GetForUpdate(ctx, customerID, model.ParentEstimate, estimateID)
		if err != nil {
			return err
		}
		if est.Status != model.StatusDraft && est.Status != model.StatusOpen && est.Status != model.StatusAccepted {
			return nil
		}
		converted, err := tx.HasConvertedInvoice(ctx, est.ID)
		if err != nil || converted {
			return err
		}
		if est.Status != model.StatusAccepted {
			if err := tx.UpdateStatus(ctx, est.ID, model.StatusAccepted); err != nil {
				return err
			}
		}
		_, err = convertEstimate(ctx, tx, est, s.now())
		return err
	})
	if err != nil {
		return fmt.Errorf("complete work: %w", err)
	}
	return nil
}

// Get returns one of the customer's bookings.
func (s *BookingService) Get(ctx context.Context, customerID, id int64) (model.Booking, error) {
	b, err := s.docs.GetBooking(ctx, customerID, id)
	if err != nil {
		return model.Booking{}, fmt.Errorf("get booking: %w", err)
	}
	return b, nil
}

// List returns the customer's upcoming live bookings.
func (s *BookingService) List(ctx context.Context, customerID int64) ([]model.Booking, error) {
	bs, err := s.docs.ListUpcoming(ctx, customerID, s.now())
	if err != nil {
		return nil, fmt.Errorf("list bookings: %w", err)
	}
	return bs, nil
}
