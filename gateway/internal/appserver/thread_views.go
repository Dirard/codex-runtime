package appserver

import (
	"encoding/json"
	"fmt"
	"time"
)

type ThreadView struct {
	ID              string
	Status          string
	Preview         string
	Ephemeral       bool
	CWD             string
	CreatedAtUnixMS int64
	UpdatedAtUnixMS int64
	Turns           []TurnView
}

type TurnView struct {
	ID                string
	Status            string
	ItemsView         string
	StartedAtUnixMS   int64
	CompletedAtUnixMS int64
	DurationMS        int64
	ErrorMessage      string
}

type ThreadTurnsPage struct {
	Turns           []TurnView
	NextCursor      string
	BackwardsCursor string
}

type threadResponseWire struct {
	Thread threadWire `json:"thread"`
}

type threadTurnsPageWire struct {
	Data            []turnWire `json:"data"`
	NextCursor      string     `json:"nextCursor"`
	BackwardsCursor string     `json:"backwardsCursor"`
}

type threadWire struct {
	ID        string          `json:"id"`
	Preview   string          `json:"preview"`
	Ephemeral bool            `json:"ephemeral"`
	CWD       string          `json:"cwd"`
	CreatedAt int64           `json:"createdAt"`
	UpdatedAt int64           `json:"updatedAt"`
	Status    json.RawMessage `json:"status"`
	Turns     []turnWire      `json:"turns"`
}

type turnWire struct {
	ID          string          `json:"id"`
	Status      string          `json:"status"`
	ItemsView   string          `json:"itemsView"`
	StartedAt   *int64          `json:"startedAt"`
	CompletedAt *int64          `json:"completedAt"`
	DurationMS  *int64          `json:"durationMs"`
	Error       json.RawMessage `json:"error"`
}

func ParseThreadView(raw json.RawMessage) (ThreadView, error) {
	var response threadResponseWire
	if err := json.Unmarshal(raw, &response); err != nil {
		return ThreadView{}, fmt.Errorf("parse thread response: %w", err)
	}
	return response.Thread.view()
}

func ParseThreadTurnsPage(raw json.RawMessage) (ThreadTurnsPage, error) {
	var page threadTurnsPageWire
	if err := json.Unmarshal(raw, &page); err != nil {
		return ThreadTurnsPage{}, fmt.Errorf("parse thread turns page: %w", err)
	}
	result := ThreadTurnsPage{
		NextCursor:      page.NextCursor,
		BackwardsCursor: page.BackwardsCursor,
		Turns:           make([]TurnView, 0, len(page.Data)),
	}
	for _, turn := range page.Data {
		result.Turns = append(result.Turns, turn.view())
	}
	return result, nil
}

func (t threadWire) view() (ThreadView, error) {
	if t.ID == "" {
		return ThreadView{}, fmt.Errorf("thread response missing id")
	}
	view := ThreadView{
		ID:              t.ID,
		Status:          parseThreadStatus(t.Status),
		Preview:         t.Preview,
		Ephemeral:       t.Ephemeral,
		CWD:             t.CWD,
		CreatedAtUnixMS: secondsToMillis(t.CreatedAt),
		UpdatedAtUnixMS: secondsToMillis(t.UpdatedAt),
		Turns:           make([]TurnView, 0, len(t.Turns)),
	}
	for _, turn := range t.Turns {
		view.Turns = append(view.Turns, turn.view())
	}
	return view, nil
}

func (t turnWire) view() TurnView {
	view := TurnView{
		ID:           t.ID,
		Status:       t.Status,
		ItemsView:    t.ItemsView,
		ErrorMessage: parseTurnErrorMessage(t.Error),
	}
	if t.StartedAt != nil {
		view.StartedAtUnixMS = secondsToMillis(*t.StartedAt)
	}
	if t.CompletedAt != nil {
		view.CompletedAtUnixMS = secondsToMillis(*t.CompletedAt)
	}
	if t.DurationMS != nil {
		view.DurationMS = *t.DurationMS
	}
	return view
}

func parseThreadStatus(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var tagged struct {
		Type string `json:"type"`
	}
	if json.Unmarshal(raw, &tagged) == nil && tagged.Type != "" {
		return tagged.Type
	}
	var value string
	if json.Unmarshal(raw, &value) == nil {
		return value
	}
	return ""
}

func parseTurnErrorMessage(raw json.RawMessage) string {
	return ""
}

func secondsToMillis(seconds int64) int64 {
	if seconds <= 0 {
		return 0
	}
	return seconds * int64(time.Second/time.Millisecond)
}
