package artifacts

import "time"

type RetentionClass string

const (
	RetentionSession RetentionClass = "session"
	RetentionTask    RetentionClass = "task"
	RetentionLong    RetentionClass = "long"
)

type EvidenceArtifact struct {
	ArtifactID      string         `json:"artifact_id"`
	ContentDigest   string         `json:"content_digest"`
	MediaType       string         `json:"media_type"`
	Encoding        string         `json:"encoding"`
	OriginalBytes   int64          `json:"original_bytes"`
	CompressedBytes int64          `json:"compressed_bytes,omitempty"`
	EstimatedTokens int            `json:"estimated_tokens"`
	StoragePath     string         `json:"storage_path"`
	SourceEventID   string         `json:"source_event_id,omitempty"`
	Command         string         `json:"command,omitempty"`
	Cwd             string         `json:"cwd,omitempty"`
	ExitCode        int            `json:"exit_code"`
	CreatedAt       time.Time      `json:"created_at"`
	RetentionClass  RetentionClass `json:"retention_class"`
	RedactionStatus string         `json:"redaction_status,omitempty"`
}

type ReductionRecord struct {
	ReductionID        string   `json:"reduction_id"`
	ArtifactID         string   `json:"artifact_id"`
	ReducerName        string   `json:"reducer_name"`
	ReducerVersion     string   `json:"reducer_version"`
	CompactContent     string   `json:"compact_content"`
	StructuredFacts    []string `json:"structured_facts,omitempty"`
	PreservedAnchors   []string `json:"preserved_anchors,omitempty"`
	OmittedLineRanges  [][2]int `json:"omitted_line_ranges,omitempty"`
	OriginalBytes      int64    `json:"original_bytes"`
	CompactBytes       int64    `json:"compact_bytes"`
	OriginalTokenEst   int      `json:"original_token_estimate"`
	CompactTokenEst    int      `json:"compact_token_estimate"`
	ReplacementApplied bool     `json:"replacement_applied"`
	Reason             string   `json:"reason,omitempty"`
}
