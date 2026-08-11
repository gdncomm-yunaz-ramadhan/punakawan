package worklogalloc

import "testing"

func TestAllocateSplitsEvenlyAcrossMatchedSubtasks(t *testing.T) {
	subtasks := []Subtask{
		{Key: "PAY-2", Summary: "Development work"},
		{Key: "PAY-3", Summary: "Testing"},
		{Key: "PAY-4", Summary: "Code review"},
	}

	got := Allocate(9, subtasks)

	if got.TotalHours != 9 {
		t.Fatalf("TotalHours = %v, want 9", got.TotalHours)
	}
	if got.UnmappedHours != 0 {
		t.Fatalf("UnmappedHours = %v, want 0", got.UnmappedHours)
	}
	if len(got.Worklogs) != 3 {
		t.Fatalf("len(Worklogs) = %d, want 3: %+v", len(got.Worklogs), got.Worklogs)
	}

	want := map[Bucket]struct {
		key   string
		hours float64
	}{
		BucketDev:    {"PAY-2", 3},
		BucketTest:   {"PAY-3", 3},
		BucketReview: {"PAY-4", 3},
	}
	for _, w := range got.Worklogs {
		exp, ok := want[w.Bucket]
		if !ok {
			t.Fatalf("unexpected bucket %q in %+v", w.Bucket, got.Worklogs)
		}
		if w.SubtaskKey != exp.key {
			t.Errorf("bucket %s: SubtaskKey = %q, want %q", w.Bucket, w.SubtaskKey, exp.key)
		}
		if w.Hours != exp.hours {
			t.Errorf("bucket %s: Hours = %v, want %v", w.Bucket, w.Hours, exp.hours)
		}
	}
}

func TestAllocateOnlySplitsAcrossBucketsWithAMatch(t *testing.T) {
	// Only a dev-named subtask exists; test/review buckets have nothing
	// to attach to, so the total must not be spread across three
	// buckets when only one is real.
	subtasks := []Subtask{{Key: "PAY-2", Summary: "Development"}}

	got := Allocate(6, subtasks)

	if len(got.Worklogs) != 1 {
		t.Fatalf("len(Worklogs) = %d, want 1: %+v", len(got.Worklogs), got.Worklogs)
	}
	if got.Worklogs[0].Bucket != BucketDev || got.Worklogs[0].Hours != 6 {
		t.Fatalf("unexpected single worklog: %+v", got.Worklogs[0])
	}
	if got.UnmappedHours != 0 {
		t.Fatalf("UnmappedHours = %v, want 0", got.UnmappedHours)
	}
}

func TestAllocateReportsUnmappedWhenNoSubtaskMatchesAnyBucket(t *testing.T) {
	subtasks := []Subtask{{Key: "PAY-9", Summary: "Deploy to staging"}}

	got := Allocate(4, subtasks)

	if len(got.Worklogs) != 0 {
		t.Fatalf("expected no matched worklogs, got %+v", got.Worklogs)
	}
	if got.UnmappedHours != 4 {
		t.Fatalf("UnmappedHours = %v, want 4", got.UnmappedHours)
	}
}

func TestAllocateZeroHoursYieldsNoWorklogs(t *testing.T) {
	got := Allocate(0, []Subtask{{Key: "PAY-2", Summary: "Development"}})
	if len(got.Worklogs) != 0 || got.UnmappedHours != 0 || got.TotalHours != 0 {
		t.Fatalf("expected an empty allocation for zero hours, got %+v", got)
	}
}

func TestAllocateFirstMatchingSubtaskWinsPerBucket(t *testing.T) {
	subtasks := []Subtask{
		{Key: "PAY-2", Summary: "Dev work part 1"},
		{Key: "PAY-3", Summary: "Dev work part 2"},
	}

	got := Allocate(5, subtasks)

	if len(got.Worklogs) != 1 {
		t.Fatalf("len(Worklogs) = %d, want 1: %+v", len(got.Worklogs), got.Worklogs)
	}
	if got.Worklogs[0].SubtaskKey != "PAY-2" {
		t.Fatalf("SubtaskKey = %q, want first match PAY-2", got.Worklogs[0].SubtaskKey)
	}
}

func TestAllocateClassificationIsCaseInsensitive(t *testing.T) {
	subtasks := []Subtask{{Key: "PAY-2", Summary: "DEVELOPMENT"}}
	got := Allocate(2, subtasks)
	if len(got.Worklogs) != 1 || got.Worklogs[0].Bucket != BucketDev {
		t.Fatalf("expected case-insensitive dev match, got %+v", got)
	}
}
