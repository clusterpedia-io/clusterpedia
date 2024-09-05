package queue

type ActionType string

const (
	Added   ActionType = "Added"
	Updated ActionType = "Updated"
	Deleted ActionType = "Deleted"
)

type Event struct {
	reputCount int

	Action ActionType
	Object interface{}
}

func (event Event) GetReputCount() int {
	return event.reputCount
}

func pressureEvents(older *Event, newer *Event) *Event {
	if newer == nil {
		return older
	}
	if older == nil || newer.Action == Deleted || older.Action == newer.Action {
		return newer
	}

	switch newer.Action {
	case Updated:
		if older.Action == Deleted {
			// TODO: can compare resource version
			// but the data obtained from the informer should be in order, so comparison is not needed.
			return older
		}

		if older.Action == Added {
			newer.Action = Added
		}
		return newer
	case Added:
		if older.Action == Deleted {
			newer.Action = Updated
			return newer
		}

		/*
			if older.Action == Updated {
				// TODO(iceber)
				// 正常来说 Updated -> Added 之间，会存在一个 Deleted 事件
			}
		*/
		return newer
	}

	return nil
}
