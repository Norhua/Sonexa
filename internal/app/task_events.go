package app

type TaskChange struct {
	Revision int64 `json:"revision"`
}

func (a *App) attachTaskNotifications() {
	a.store.SetTaskChanged(func(revision int64) {
		change := TaskChange{Revision: revision}
		a.emitEvent(EventReviewChanged, change)
		a.emitEvent(EventTasksChanged, change)
	})
}
