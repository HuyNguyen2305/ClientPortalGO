package service

import (
	"context"
	"fmt"
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
	now      func() time.Time
}

// NewBookingService returns a booking service.
func NewBookingService(docs BillingDocumentRepository, locs LocationRepository, services ServiceRepository,
	hours model.Hours, now func() time.Time) *BookingService {
	return &BookingService{docs: docs, locs: locs, services: services, hours: hours, now: now}
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

// Slots returns the free start times for a service on a date.
func (s *BookingService) Slots(ctx context.Context, serviceID int64, date string) (time.Time, []string, error) {
	day, err := model.ParseDate(date)
	if err != nil {
		return time.Time{}, nil, err
	}
	if err := s.hours.CheckBookable(s.now(), day); err != nil {
		return time.Time{}, nil, err
	}
	svc, err := s.services.Get(ctx, serviceID)
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
	svc, err := s.services.Get(ctx, in.ServiceID)
	if err != nil {
		return model.Booking{}, fmt.Errorf("book: %w", err)
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

	var id int64
	err = s.docs.WithTx(ctx, func(tx BillingDocumentRepository) error {
		estimateID, err := tx.Create(ctx, model.BillingDocument{
			ParentType: model.ParentEstimate,
			CustomerID: customerID,
			Location:   model.Location{ID: in.LocationID},
			Title:      svc.Name,
			// The customer's confirmation is the request, so the estimate starts open.
			Status:       model.StatusOpen,
			DocumentDate: day,
			TotalCents:   svc.PriceCents,
			Items:        []model.Item{{Name: svc.Name, Quantity: 1, UnitPriceCents: svc.PriceCents}},
		})
		if err != nil {
			return err
		}
		id, err = tx.CreateBooking(ctx, model.Booking{
			CustomerID: customerID,
			Service:    model.Service{ID: svc.ID},
			Location:   model.Location{ID: in.LocationID},
			Estimate:   model.BillingDocument{ID: estimateID},
			StartsAt:   start,
			EndsAt:     start.Add(time.Duration(svc.DurationMinutes) * time.Minute),
		})
		return err
	})
	if err != nil {
		return model.Booking{}, fmt.Errorf("book: %w", err)
	}
	return s.Get(ctx, customerID, id)
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
