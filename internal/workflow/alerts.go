package workflow

import "github.com/lyracorp/xmanager/internal/notify"

// AlertBridge runs enabled alert-triggered workflows whenever an alert is sent.
type AlertBridge struct {
	Inner notify.Notifier
	Eng   *Engine
}

func WrapNotifier(inner notify.Notifier, eng *Engine) notify.Notifier {
	return AlertBridge{Inner: inner, Eng: eng}
}

func (b AlertBridge) Send(alert notify.Alert) error {
	if b.Eng != nil {
		b.Eng.TriggerAlert(alert.Title + " " + alert.Message)
	}
	if b.Inner != nil {
		return b.Inner.Send(alert)
	}
	return nil
}

func (b AlertBridge) Test() error {
	if b.Inner != nil {
		return b.Inner.Test()
	}
	return nil
}

func (b AlertBridge) Name() string {
	if b.Inner != nil {
		return b.Inner.Name()
	}
	return "workflow-alerts"
}
