package service

import (
	"context"
	"fmt"
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

// BookSeries books a recurring template: one job (estimate and booking) per date of its repeat rule,
// from the customer's first slot to the end of the series or the booking horizon. A date whose slot
// is taken is skipped and reported; the rest of the series goes ahead.
func (s *BookingService) BookSeries(ctx context.Context, customerID int64, in model.BookingInput) (model.BookingSeries, error) {
	svc, err := s.bookable(ctx, in.ServiceID)
	if err != nil {
		return model.BookingSeries{}, fmt.Errorf("book series: %w", err)
	}
	if !svc.Recurrence.Repeats() {
		return model.BookingSeries{}, fmt.Errorf("%w: service %d does not repeat; book it with /bookings", model.ErrValidation, svc.ID)
	}
	first, err := s.hours.ParseSlot(s.now(), in.Date, in.Time, svc.DurationMinutes)
	if err != nil {
		return model.BookingSeries{}, err
	}
	if _, err := s.locs.Get(ctx, customerID, in.LocationID); err != nil {
		return model.BookingSeries{}, fmt.Errorf("book series: location: %w", err)
	}
	live, err := s.docs.ListUpcoming(ctx, customerID, s.now())
	if err != nil {
		return model.BookingSeries{}, fmt.Errorf("book series: %w", err)
	}
	for _, b := range live {
		if b.SeriesID != nil && b.Service.ID == svc.ID {
			return model.BookingSeries{}, fmt.Errorf("%w: you already have an upcoming series of service %d", model.ErrConflict, svc.ID)
		}
	}

	firstDay, err := model.ParseDate(in.Date)
	if err != nil {
		return model.BookingSeries{}, err
	}
	_, lastDay := s.hours.BookableRange(s.now())
	days := svc.Recurrence.Days(firstDay, lastDay)
	if len(days) == 0 { // the series ended before the chosen first date
		return model.BookingSeries{}, fmt.Errorf("%w: the series ends before %s", model.ErrValidation, in.Date)
	}
	from, _ := s.hours.DayBounds(firstDay)
	_, to := s.hours.DayBounds(days[len(days)-1])
	busy, err := s.docs.BusyIntervals(ctx, from, to)
	if err != nil {
		return model.BookingSeries{}, fmt.Errorf("book series: %w", err)
	}

	type job struct{ day, start time.Time }
	var jobs []job
	var skipped []time.Time
	length := time.Duration(svc.DurationMinutes) * time.Minute
	for _, d := range days {
		start, err := s.hours.ParseSlot(s.now(), d.Format(time.DateOnly), in.Time, svc.DurationMinutes)
		if err != nil {
			return model.BookingSeries{}, err
		}
		if slices.ContainsFunc(busy, func(iv model.Interval) bool { return iv.Start.Before(start.Add(length)) && start.Before(iv.End) }) {
			skipped = append(skipped, d)
			continue
		}
		jobs = append(jobs, job{d, start})
	}
	if len(jobs) == 0 {
		return model.BookingSeries{}, fmt.Errorf("%w: every date of the series is already taken", model.ErrConflict)
	}

	var seriesID int64
	// shortcut: a slot taken between the check above and the insert fails the whole series with
	// ErrConflict (bookings_no_overlap); retry per job with a savepoint if that ever matters.
	err = s.docs.WithTx(ctx, func(tx BillingDocumentRepository) error {
		var err error
		if seriesID, err = tx.CreateSeries(ctx, customerID, in.LocationID, svc.ID, first); err != nil {
			return err
		}
		for _, j := range jobs {
			if _, err := s.createJob(ctx, tx, svc, customerID, in.LocationID, j.day, j.start, &seriesID); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return model.BookingSeries{}, fmt.Errorf("book series: %w", err)
	}
	made, err := s.docs.ListSeriesBookings(ctx, customerID, seriesID)
	if err != nil {
		return model.BookingSeries{}, fmt.Errorf("book series: %w", err)
	}
	return model.BookingSeries{ID: seriesID, Jobs: made, Skipped: skipped}, nil
}

// Cancel cancels one of the customer's live bookings and declines its estimate, freeing the slot.
// It works while the estimate is open or accepted, but not once it became an invoice. The status
// is set directly: the public /decline transition keeps `accepted` terminal.
func (s *BookingService) Cancel(ctx context.Context, customerID, id int64) (model.Booking, error) {
	err := s.docs.WithTx(ctx, func(tx BillingDocumentRepository) error {
		b, err := tx.GetBooking(ctx, customerID, id)
		if err != nil {
			return err
		}
		if b.CancelledAt != nil {
			return fmt.Errorf("%w: booking %d is already cancelled", model.ErrConflict, id)
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
		if est.Status != model.StatusOpen && est.Status != model.StatusAccepted {
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

// Complete marks an in-progress work order completed, saving the notes when given.
func (s *BookingService) Complete(ctx context.Context, id int64, notes *string) error {
	if err := s.docs.AdvanceWork(ctx, id, model.WorkInProgress, model.WorkCompleted, notes); err != nil {
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
