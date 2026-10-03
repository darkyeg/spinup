package library

import "time"

// Stamp is when your library last changed, and what it held then. The zero Stamp is a library you never
// changed: it never wins against one you did.
type Stamp struct {
	ChangedAt time.Time `json:"changed_at"`
	Hash      string    `json:"hash"`
}

// futureSlack allows for clocks that differ a little between machines.
const futureSlack = time.Minute

// FromTheFuture reports a stamp dated later than now allows: it would win against every later change.
func (s Stamp) FromTheFuture(now time.Time) bool {
	return s.ChangedAt.After(now.Add(futureSlack))
}

// Move is what a machine does with its library after comparing it with another machine's.
type Move int

const (
	Stay Move = iota
	// Take the other machine's library: it is newer.
	Take
	// Give this machine's library to the other one: it is newer.
	Give
)

// Compare decides between two libraries: the one changed last wins; equal times fall back to the hash, so
// both sides pick the same one.
func Compare(here, there Stamp) Move {
	switch {
	case here.Hash == there.Hash:
		return Stay
	case there.ChangedAt.After(here.ChangedAt):
		return Take
	case here.ChangedAt.After(there.ChangedAt):
		return Give
	case there.Hash > here.Hash:
		return Take
	}
	return Give
}
