// Package collectorreview exposes staged collector evidence for operator inspection.
// It deliberately contains no publication or review-state mutations.
package collectorreview

import (
	"context"
	"errors"
	"time"
)

var ErrNotFound = errors.New("collector candidate not found")

type Query struct {
	SourceID string
	BatchID  string
	Status   string
	Search   string
	Limit    int
	Offset   int
}

type Batch struct {
	BatchID        string    `json:"batch_id"`
	SourceID       string    `json:"source_id"`
	SourceName     string    `json:"source_name"`
	Region         string    `json:"region"`
	Status         string    `json:"status"`
	InputCount     int       `json:"input_count"`
	InsertedCount  int       `json:"inserted_count"`
	DuplicateCount int       `json:"duplicate_count"`
	CandidateCount int       `json:"candidate_count"`
	PendingCount   int       `json:"pending_count"`
	RequestHash    string    `json:"request_hash"`
	ReceivedAt     time.Time `json:"received_at"`
}

type Candidate struct {
	CandidateID       string    `json:"candidate_id"`
	SourceRecordID    string    `json:"source_record_id"`
	BatchID           string    `json:"batch_id"`
	SourceID          string    `json:"source_id"`
	SourceName        string    `json:"source_name"`
	ExternalID        string    `json:"external_id"`
	Name              string    `json:"name"`
	Aliases           []string  `json:"aliases"`
	ProfileURL        string    `json:"profile_url"`
	SourceContentHash string    `json:"source_content_hash"`
	ContentHash       string    `json:"content_hash"`
	ConnectorVersion  string    `json:"connector_version"`
	CheckedAt         string    `json:"checked_at"`
	ReceivedAt        time.Time `json:"received_at"`
	CandidateStatus   string    `json:"candidate_status"`
	RightsStatus      string    `json:"rights_status"`
	PublicationStatus string    `json:"publication_status"`
	AdultStatus       string    `json:"adult_status"`
}

type BatchPage struct {
	Items      []Batch `json:"items"`
	Total      int64   `json:"total"`
	NextOffset *int    `json:"next_offset"`
}

type CandidatePage struct {
	Items      []Candidate `json:"items"`
	Total      int64       `json:"total"`
	NextOffset *int        `json:"next_offset"`
}

type Repository interface {
	ListCollectorBatches(context.Context, Query) (BatchPage, error)
	ListCollectorCandidates(context.Context, Query) (CandidatePage, error)
	CollectorCandidate(context.Context, string) (Candidate, error)
}
