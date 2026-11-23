package domain

import (
	"time"

	"github.com/kakkky/gorviz/internal/domain/observation"
)

type DestinationKind string

const (
	DestinationWebhook DestinationKind = "webhook"
	DestinationSlack   DestinationKind = "slack"
	DestinationMail    DestinationKind = "mail"
)

// Destination は通知の送り先 1 つ。Webhook と Slack は URL、メールは SMTP の口と宛先を使う
type Destination struct {
	Kind     DestinationKind `json:"kind"`
	URL      string          `json:"url,omitempty"`
	SMTPAddr string          `json:"smtpAddr,omitempty"`
	Username string          `json:"username,omitempty"`
	Password string          `json:"password,omitempty"`
	From     string          `json:"from,omitempty"`
	To       []string        `json:"to,omitempty"`
}

type Notification struct {
	Service    observation.ServiceName  `json:"service"`
	Issue      string                   `json:"issue"`
	Kind       IssueKind                `json:"kind"`
	Subject    string                   `json:"subject"`
	Certainty  Certainty                `json:"certainty"`
	StartedAt  time.Time                `json:"startedAt"`
	Instance   observation.InstanceName `json:"instance"`
	Reason     string                   `json:"reason"`
	NextAction string                   `json:"nextAction"`
	URL        string                   `json:"url"`
}

func NewNotification(service observation.ServiceName, o TrackedOccurrence, gui GUI) Notification {
	return Notification{
		Service: service, Issue: o.Issue, Kind: o.Kind, Subject: o.Subject, Certainty: o.Occurrence.Certainty,
		StartedAt: o.Occurrence.StartedAt, Instance: o.Occurrence.Instance, Reason: o.Occurrence.Reason,
		NextAction: o.Kind.NextAction(), URL: gui.Issue(service, o.Issue),
	}
}
