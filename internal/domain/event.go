package domain

import "time"

// Event is an entry of the admin's activity feed.
type Event struct {
	ID       int64     `json:"id"`
	At       time.Time `json:"at"`
	Kind     string    `json:"kind"`
	By       string    `json:"by,omitempty"`
	Bundle   string    `json:"bundle,omitempty"`
	Token    string    `json:"token,omitempty"`
	Revision string    `json:"revision,omitempty"`
	Note     string    `json:"note,omitempty"`
	Code     string    `json:"code,omitempty"`
	Rule     string    `json:"rule,omitempty"`
	Reason   string    `json:"reason,omitempty"`
}
