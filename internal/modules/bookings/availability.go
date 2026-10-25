package bookings

import (
	"fmt"
	"sort"
	"time"
)

// Availability Engine — pure functions ทำงานเป็น "นาทีจากเที่ยงคืน" เพื่อให้ทดสอบได้ง่าย
// ข้อมูลวันปิด/วันลา/เวลาปัจจุบันถูกเตรียมโดย caller ก่อนเข้า engine

// Interval ช่วงเวลา [Start, End) เป็นนาทีจากเที่ยงคืน
type Interval struct {
	Start int
	End   int
}

// StaffSlots ข้อมูลความว่างของช่างหนึ่งคนในหนึ่งวัน
type StaffSlots struct {
	StaffID   string
	Window    *Interval // nil = ไม่มีกะทำงาน
	Busy      []Interval
	BookedCount int // จำนวนคิวในวันนั้น (ใช้จัดลำดับ auto-assign)
	Rank      int // display_order สำรอง (น้อย = มาก่อน)
}

// EngineInput ข้อมูลที่ engine ต้องการ (caller โหลดจาก DB แล้ว)
type EngineInput struct {
	// StoreWindow = ช่วงเวลาเปิดร้านของวันนั้น (nil = ร้านปิด)
	StoreWindow *Interval
	Staff       []StaffSlots
	DurationMin int // ระยะเวลารวมของบริการที่เลือก
	SlotStepMin int // ระยะห่างของ slot ที่แสดง (15 นาที)
	// MinStart = นาทีต่ำสุดที่ยังจองได้ (ใช้กับวันนี้: เวลาปัจจุบัน) — nil = ไม่จำกัด
	MinStart *int
}

const defaultSlotStep = 15

// StaffFitsAt เช็คว่า [start, start+duration) อยู่ในกะและไม่ทับคิวที่มีอยู่
func StaffFitsAt(s StaffSlots, start, duration int) bool {
	if s.Window == nil {
		return false
	}
	end := start + duration
	if start < s.Window.Start || end > s.Window.End {
		return false
	}
	for _, b := range s.Busy {
		if start < b.End && b.Start < end {
			return false
		}
	}
	return true
}

// ComputeAllSlots คืน slot เวลาที่จองได้ (union ของทุกช่าง) และ map ของช่างที่ว่างแต่ละ slot
func ComputeAllSlots(in EngineInput) (slots []string, freeStaffAt map[string][]StaffSlots) {
	step := in.SlotStepMin
	if step <= 0 {
		step = defaultSlotStep
	}

	freeStaffAt = map[string][]StaffSlots{}
	slotSet := map[int]bool{}

	for _, staff := range in.Staff {
		if in.StoreWindow == nil || staff.Window == nil {
			continue
		}
		windowStart := staff.Window.Start
		if in.StoreWindow.Start > windowStart {
			windowStart = in.StoreWindow.Start
		}
		windowEnd := staff.Window.End
		if in.StoreWindow.End < windowEnd {
			windowEnd = in.StoreWindow.End
		}
		effective := Interval{Start: windowStart, End: windowEnd}
		if effective.End <= effective.Start {
			continue
		}

		for start := effective.Start; start+in.DurationMin <= effective.End; start += step {
			if in.MinStart != nil && start < *in.MinStart {
				continue
			}
			if StaffFitsAt(StaffSlots{StaffID: staff.StaffID, Window: &effective, Busy: staff.Busy}, start, in.DurationMin) {
				key := fmt.Sprintf("%02d:%02d", start/60, start%60)
				slotSet[start] = true
				freeStaffAt[key] = append(freeStaffAt[key], staff)
			}
		}
	}

	starts := make([]int, 0, len(slotSet))
	for s := range slotSet {
		starts = append(starts, s)
	}
	sort.Ints(starts)
	slots = make([]string, 0, len(starts))
	for _, s := range starts {
		slots = append(slots, fmt.Sprintf("%02d:%02d", s/60, s%60))
	}
	return slots, freeStaffAt
}

// PickStaff เลือกช่างจากรายชื่อที่ว่าง: คิวน้อยที่สุดในวันนั้นก่อน ตามด้วยลำดับ display_order
func PickStaff(candidates []StaffSlots) *StaffSlots {
	if len(candidates) == 0 {
		return nil
	}
	best := candidates[0]
	for _, c := range candidates[1:] {
		if c.BookedCount < best.BookedCount ||
			(c.BookedCount == best.BookedCount && c.Rank < best.Rank) {
			best = c
		}
	}
	return &best
}

// ParseClock แปลง "HH:MM" เป็นนาทีจากเที่ยงคืน
func ParseClock(s string) (int, error) {
	t, err := time.Parse("15:04", s)
	if err != nil {
		return 0, err
	}
	return t.Hour()*60 + t.Minute(), nil
}
