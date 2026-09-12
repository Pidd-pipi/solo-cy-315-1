package constants

// Pagination defaults and limits.
const (
	DefaultPage     = 1
	DefaultPageSize = 20
	MaxPageSize     = 200
)

// Adjustment action names.
const (
	ActionSwap = "swap"
	ActionMove = "move"
)

// Schedule version lifecycle statuses. A version starts as draft, becomes
// published exactly once at a time, and turns archived when a newer version
// is published.
const (
	VersionStatusDraft     = "draft"
	VersionStatusPublished = "published"
	VersionStatusArchived  = "archived"
)
