package knowledge

import (
	"testing"
)

func TestPutEmitsAKnowledgeEvent(t *testing.T) {
	store := newTestStore(t)

	rec := validRecord()
	if err := store.Put(rec); err != nil {
		t.Fatalf("Put: %v", err)
	}

	events, err := store.Events()
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	if len(events) != 1 || events[0].Type != EventTypePut || events[0].RecordId != rec.Id {
		t.Fatalf("events = %+v, want one put event for %s", events, rec.Id)
	}
}

func TestSupersedeEmitsAPutAndASupersedeEvent(t *testing.T) {
	store := newTestStore(t)

	rec := validRecord()
	if err := store.Put(rec); err != nil {
		t.Fatalf("Put: %v", err)
	}

	newer := validRecord()
	newer.Id = "pkw:req/fixture/REQ-2"
	if err := store.Put(newer); err != nil {
		t.Fatalf("Put newer: %v", err)
	}

	if err := store.Supersede(rec.Id, newer.Id); err != nil {
		t.Fatalf("Supersede: %v", err)
	}

	events, err := store.Events()
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	// 2 puts (rec, newer) + Supersede's own Put + Supersede's supersede event.
	if len(events) != 4 {
		t.Fatalf("events = %+v, want 4 events", events)
	}
	last := events[len(events)-1]
	if last.Type != EventTypeSupersede || last.RecordId != rec.Id || last.SupersededBy != newer.Id {
		t.Fatalf("last event = %+v, want a supersede event for %s -> %s", last, rec.Id, newer.Id)
	}
}

func TestDeleteEmitsADeleteEvent(t *testing.T) {
	store := newTestStore(t)

	rec := validRecord()
	if err := store.Put(rec); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := store.Delete(rec.Id); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	events, err := store.Events()
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("events = %+v, want 2 events (put, delete)", events)
	}
	last := events[len(events)-1]
	if last.Type != EventTypeDelete || last.RecordId != rec.Id || last.RecordType != rec.Type {
		t.Fatalf("last event = %+v, want a delete event for %s", last, rec.Id)
	}
}

func TestDeleteOfMissingIdDoesNotEmitEvent(t *testing.T) {
	store := newTestStore(t)

	if err := store.Delete("pkw:req/fixture/does-not-exist"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	events, err := store.Events()
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	if len(events) != 0 {
		t.Fatalf("events = %+v, want none for deleting a nonexistent id", events)
	}
}
