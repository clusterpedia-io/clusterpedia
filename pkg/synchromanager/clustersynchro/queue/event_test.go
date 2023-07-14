package queue

import (
	"testing"
)

type pressureEventsTest struct {
	name       string
	older      *Event
	newer      *Event
	reputCount int
}

func TestPressureEvents(t *testing.T) {
	tests := []pressureEventsTest{
		{
			name: "Newer event is not nil",
			older: &Event{
				reputCount: 1,
				Action:     Added,
				Object:     "Object1",
			},
			newer: &Event{
				reputCount: 2,
				Action:     Updated,
				Object:     "Object2",
			},
			reputCount: 2,
		},
		{
			name:       "Newer event is nil",
			older:      &Event{reputCount: 3, Action: Added, Object: "Object3"},
			newer:      nil,
			reputCount: 3,
		},
		{
			name:       "Older event is nil",
			older:      nil,
			newer:      &Event{reputCount: 4, Action: Updated, Object: "Object4"},
			reputCount: 4,
		},
		{
			name: "Newer action is Deleted",
			older: &Event{
				reputCount: 5,
				Action:     Added,
				Object:     "Object5",
			},
			newer: &Event{
				reputCount: 6,
				Action:     Deleted,
				Object:     "Object6",
			},
			reputCount: 6,
		},
		{
			name: "Older action is Updated and newer action is Added",
			older: &Event{
				reputCount: 7,
				Action:     Updated,
				Object:     "Object7",
			},
			newer: &Event{
				reputCount: 8,
				Action:     Added,
				Object:     "Object8",
			},
			reputCount: 8,
		},
		{
			name: "Newer action is Updated and older action is Deleted",
			older: &Event{
				reputCount: 9,
				Action:     Deleted,
				Object:     "Object9",
			},
			newer: &Event{
				reputCount: 10,
				Action:     Updated,
				Object:     "Object10",
			},
			reputCount: 9,
		},
		{
			name: "Newer action is Updated and older action is Updated",
			older: &Event{
				reputCount: 11,
				Action:     Updated,
				Object:     "Object11",
			},
			newer: &Event{
				reputCount: 12,
				Action:     Updated,
				Object:     "Object12",
			},
			reputCount: 12,
		},
		{
			name: "Newer action is Added and older action is Deleted",
			older: &Event{
				reputCount: 13,
				Action:     Deleted,
				Object:     "Object13",
			},
			newer: &Event{
				reputCount: 14,
				Action:     Added,
				Object:     "Object14",
			},
			reputCount: 14,
		},
		{
			name: "Newer action is Added and older action is Added",
			older: &Event{
				reputCount: 15,
				Action:     Added,
				Object:     "Object15",
			},
			newer: &Event{
				reputCount: 16,
				Action:     Added,
				Object:     "Object16",
			},
			reputCount: 16,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := pressureEvents(test.older, test.newer)
			// Check if the reputation count is correct
			if result.GetReputCount() != test.reputCount {
				t.Errorf("Expected reputCount to be %d, but got %d", test.reputCount, result.GetReputCount())
			}
		})
	}
}
