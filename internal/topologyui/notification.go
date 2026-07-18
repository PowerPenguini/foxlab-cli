package topologyui

import "foxlab-cli/internal/topology"

type NotificationLevel uint8

const (
	NotificationError NotificationLevel = iota
	NotificationInfo
	NotificationSuccess
)

type Notification struct {
	Text     string
	Level    NotificationLevel
	Busy     bool
	Revision uint64
}

func (a *App) setNotification(notification Notification) {
	a.notificationState.nextRevision++
	if a.notificationState.nextRevision == 0 {
		a.notificationState.nextRevision++
	}
	notification.Revision = a.notificationState.nextRevision
	a.State.Notification = notification
	a.State.Message = notification.Text
}

func (a *App) setOperationResult(result topology.Result) {
	level := NotificationError
	switch result.Kind {
	case topology.ResultInfo:
		level = NotificationInfo
	case topology.ResultSuccess:
		level = NotificationSuccess
	}
	a.setNotification(Notification{Text: result.Message, Level: level})
}

func notificationFromState(state ViewState) (Notification, bool) {
	if state.Message == "" {
		return Notification{}, false
	}
	if state.Notification.Revision != 0 && state.Notification.Text == state.Message {
		return state.Notification, true
	}
	return Notification{Text: state.Message}, true
}
