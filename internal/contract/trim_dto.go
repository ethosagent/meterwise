package contract

import (
	"reflect"
	"time"
)

// TrimStats is GET /api/workspaces/{id}/trim/stats?range=. SavedUsd is certain, LikelySavedUsd likely and
// WouldSaveUsd what observe-mode calls would have saved: three numbers, never summed (ADR 0023). ByStage has the
// five stages in their fixed order; ByTool at most 20 tools, tokens descending.
type TrimStats struct {
	Range           string          `json:"range"`
	From            string          `json:"from"`
	To              string          `json:"to"`
	Mode            TrimMode        `json:"mode"`
	Calls           int64           `json:"calls"`
	TrimmedCalls    int64           `json:"trimmedCalls"`
	SavedUsd        MicroUSD        `json:"savedUsd"`
	LikelySavedUsd  MicroUSD        `json:"likelySavedUsd"`
	WouldSaveUsd    MicroUSD        `json:"wouldSaveUsd"`
	TrimTokens      int64           `json:"trimTokens"`
	WouldTrimTokens int64           `json:"wouldTrimTokens"`
	ByStage         []TrimStageStat `json:"byStage"`
	ByTool          []TrimToolStat  `json:"byTool"`
}

// TrimStageStat is one stage's counts over the range.
type TrimStageStat struct {
	Stage      TrimStage `json:"stage"`
	Fires      int64     `json:"fires"`
	Reverted   int64     `json:"reverted"`
	Tokens     int64     `json:"tokens"`
	BytesSaved int64     `json:"bytesSaved"`
}

// TrimToolStat is one tool's trim counts over the range.
type TrimToolStat struct {
	Tool   string `json:"tool"`
	Fires  int64  `json:"fires"`
	Tokens int64  `json:"tokens"`
}

// TrimPreview is GET /api/workspaces/{id}/calls/{callId}/trim: sizes and our fixed markers only; no body is stored.
type TrimPreview struct {
	CallID     string          `json:"callId"`
	Action     *TrimAction     `json:"action"`
	BodyBefore int             `json:"bodyBefore"`
	BodyAfter  int             `json:"bodyAfter"`
	Stages     []TrimStageView `json:"stages"`
}

// TrimStageView is one stage of a call's trim. Marker is our fixed template, never request text.
type TrimStageView struct {
	Stage       TrimStage `json:"stage"`
	Tool        *string   `json:"tool"`
	Regions     int       `json:"regions"`
	BytesBefore int       `json:"bytesBefore"`
	BytesAfter  int       `json:"bytesAfter"`
	EstTokens   int64     `json:"estTokens"`
	Reverted    bool      `json:"reverted"`
	Marker      string    `json:"marker"`
}

// TrimEventRecord is one trim_events row: one non-reverted stage of one call. The ledger writes it (H4) in the
// call's transaction; tokens are the call's trim tokens split pro rata by the stage's bytes saved.
type TrimEventRecord struct {
	ID          int64
	OrgID       string
	WorkspaceID string
	SessionID   string
	CallID      string
	TS          time.Time
	Stage       TrimStage
	Tool        string
	Regions     int
	BytesBefore int
	BytesAfter  int
	EstTokens   int64
	Reverted    bool
	Action      TrimAction
}

func trimDTOs() []DTO {
	return []DTO{
		{"TrimConfig", reflect.TypeFor[TrimConfig]()},
		{"TrimConfigPatch", reflect.TypeFor[TrimConfigPatch]()},
		{"TrimStats", reflect.TypeFor[TrimStats]()},
		{"TrimStageStat", reflect.TypeFor[TrimStageStat]()},
		{"TrimToolStat", reflect.TypeFor[TrimToolStat]()},
		{"TrimPreview", reflect.TypeFor[TrimPreview]()},
		{"TrimStageView", reflect.TypeFor[TrimStageView]()},
	}
}
