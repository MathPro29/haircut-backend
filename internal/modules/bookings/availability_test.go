package bookings

import (
	"testing"
)

func ptrInt(v int) *int { return &v }

func window(s, e int) *Interval { return &Interval{Start: s, End: e} }

func TestComputeAllSlots_BasicWindow(t *testing.T) {
	// ร้าน + ช่างเปิด 10:00–20:00 บริการ 45 นาที → slot แรก 10:00, slot สุดท้ายที่ยังจบทัน 19:15
	slots, freeAt := ComputeAllSlots(EngineInput{
		StoreWindow: window(600, 1200),
		Staff: []StaffSlots{
			{StaffID: "s1", Window: window(600, 1200)},
		},
		DurationMin: 45,
	})
	if len(slots) == 0 {
		t.Fatal("expected slots, got none")
	}
	if slots[0] != "10:00" {
		t.Errorf("first slot = %s, want 10:00", slots[0])
	}
	if slots[len(slots)-1] != "19:15" {
		t.Errorf("last slot = %s, want 19:15 (45 นาที ก่อน 20:00)", slots[len(slots)-1])
	}
	if len(freeAt["10:00"]) != 1 {
		t.Errorf("expected 1 free staff at 10:00, got %d", len(freeAt["10:00"]))
	}
}

func TestComputeAllSlots_StoreClosed(t *testing.T) {
	slots, _ := ComputeAllSlots(EngineInput{
		StoreWindow: nil,
		Staff:       []StaffSlots{{StaffID: "s1", Window: window(600, 1200)}},
		DurationMin: 30,
	})
	if len(slots) != 0 {
		t.Errorf("store closed → expected 0 slots, got %d", len(slots))
	}
}

func TestComputeAllSlots_StaffNoSchedule(t *testing.T) {
	slots, _ := ComputeAllSlots(EngineInput{
		StoreWindow: window(600, 1200),
		Staff:       []StaffSlots{{StaffID: "s1", Window: nil}},
		DurationMin: 30,
	})
	if len(slots) != 0 {
		t.Errorf("staff off → expected 0 slots, got %d", len(slots))
	}
}

func TestComputeAllSlots_BusyBlocks(t *testing.T) {
	// มีคิว 11:00–12:00 — slot 10:45 (จบ 11:30 ทับ) ต้องหาย, 10:00 (จบ 10:45) และ 12:00 ยังอยู่
	slots, _ := ComputeAllSlots(EngineInput{
		StoreWindow: window(600, 1200),
		Staff: []StaffSlots{
			{StaffID: "s1", Window: window(600, 1200), Busy: []Interval{{Start: 660, End: 720}}},
		},
		DurationMin: 45,
	})
	has := func(t0 string) bool {
		for _, s := range slots {
			if s == t0 {
				return true
			}
		}
		return false
	}
	if has("10:45") {
		t.Error("10:45 overlaps busy 11:00–12:00, should be removed")
	}
	if !has("10:00") {
		t.Error("10:00 should still be available")
	}
	if !has("12:00") {
		t.Error("12:00 should still be available")
	}
}

func TestComputeAllSlots_MinStartToday(t *testing.T) {
	// วันนี้ 15:30 แล้ว — slot ก่อน 15:30 ต้องถูกตัด
	slots, _ := ComputeAllSlots(EngineInput{
		StoreWindow: window(600, 1200),
		Staff: []StaffSlots{
			{StaffID: "s1", Window: window(600, 1200)},
		},
		DurationMin: 30,
		MinStart:    ptrInt(15*60 + 30),
	})
	if len(slots) == 0 {
		t.Fatal("expected evening slots")
	}
	if slots[0] < "15:30" {
		t.Errorf("first slot = %s, want >= 15:30", slots[0])
	}
}

func TestComputeAllSlots_UnionAcrossStaff(t *testing.T) {
	// ช่าง A ว่างเช้า ช่าง B ว่างบ่าย → union ต้องได้ทั้งเช้าและบ่าย
	slots, freeAt := ComputeAllSlots(EngineInput{
		StoreWindow: window(600, 1200),
		Staff: []StaffSlots{
			{StaffID: "a", Window: window(600, 900)},
			{StaffID: "b", Window: window(900, 1200)},
		},
		DurationMin: 60,
	})
	if len(slots) == 0 {
		t.Fatal("expected union slots")
	}
	if len(freeAt["10:00"]) != 1 || freeAt["10:00"][0].StaffID != "a" {
		t.Error("10:00 should be staff a only")
	}
	if len(freeAt["15:00"]) != 1 || freeAt["15:00"][0].StaffID != "b" {
		t.Error("15:00 should be staff b only")
	}
}

func TestPickStaff_LeastBookedThenRank(t *testing.T) {
	a := StaffSlots{StaffID: "a", BookedCount: 3, Rank: 1}
	b := StaffSlots{StaffID: "b", BookedCount: 1, Rank: 5}
	c := StaffSlots{StaffID: "c", BookedCount: 1, Rank: 2}
	got := PickStaff([]StaffSlots{a, b, c})
	if got.StaffID != "c" {
		t.Errorf("PickStaff = %s, want c (คิวเท่ากัน เลือก display_order น้อย)", got.StaffID)
	}
}

func TestParseClock(t *testing.T) {
	m, err := ParseClock("14:30")
	if err != nil || m != 870 {
		t.Errorf("ParseClock(14:30) = %d, %v; want 870, nil", m, err)
	}
	if _, err := ParseClock("25:00"); err == nil {
		t.Error("ParseClock(25:00) should fail")
	}
}
