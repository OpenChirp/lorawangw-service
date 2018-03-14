//go:generate stringer -type=NotificationType

package models

import "github.com/brocaar/lorawan"

// NotificationType defines the notification.
type NotificationType int

// Available notification types.
const (
	JoinNotificationType NotificationType = iota
	ErrorNotificationType
	ACKNotificationType
)

// JoinNotification defines the payload sent to the application on
// a JoinNotificationType event.
type JoinNotification struct {
	DevAddr lorawan.DevAddr `json:"devAddr"`
	DevEUI  lorawan.EUI64   `json:"devEUI"`
}

// ACKNotification defines the payload sent to the application
// on an ACK event.
type ACKNotification struct {
	Reference string        `json:"reference"` // refers to the given reference by the application
	DevEUI    lorawan.EUI64 `json:"devEUI"`
}
