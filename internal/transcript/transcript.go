// Package transcript records harness execution sessions as JSONL event streams.
//
// Each session is a directory under the transcript storage root containing
// a meta.json file and an events.jsonl file.  The store provides create,
// list, and prune operations.
package transcript

import "time"

// Event represents a single recorded event in a session.
type Event struct {
	SessionID string    `json:"sessionId"`
	Seq       int       `json:"seq"`
	Timestamp time.Time `json:"ts"`
	Stream    string    `json:"stream"` // "stdout" or "stderr"
	Text      string    `json:"text"`
}

// Session represents a recorded execution session.
type Session struct {
	ID        string    `json:"id"`
	StartTime time.Time `json:"startTime"`
	CloseTime time.Time `json:"closeTime,omitempty"`
	BundleRef string    `json:"bundleRef"`
}
