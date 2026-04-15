package transfer

import (
	"math"
	"runtime"
	"strings"
)

const (
	DefaultSessionChunkSize          int64 = 8 << 20
	DefaultSessionInitialParallelism       = 2
	DefaultSessionMaxParallelism           = 16
	SessionAdaptivePolicyVersion           = "v2-lan-fast"
	sessionCopyBufferSize                  = 1 << 20
	sessionChunkGranularity          int64 = 256 * 1024
	sessionMinChunkSize              int64 = 1 << 20
	sessionTargetChunkWaves                = 8
)

type CompletedPart struct {
	PartIndex int
	Offset    int64
	Length    int64
}

func RecommendedSessionProfile(fileSize int64) (chunkSize int64, initial int, max int) {
	initial, max = RecommendedSessionParallelism(fileSize)
	chunkSize = RecommendedSessionChunkSize(fileSize, initial)
	return chunkSize, initial, max
}

func ClampSessionProfile(chunkSize int64, initial int, max int) (int64, int, int) {
	if chunkSize <= 0 {
		chunkSize = DefaultSessionChunkSize
	}
	if chunkSize < sessionMinChunkSize {
		chunkSize = sessionMinChunkSize
	}
	if chunkSize > DefaultSessionChunkSize {
		chunkSize = DefaultSessionChunkSize
	}
	if chunkSize > sessionMinChunkSize {
		chunkSize = roundDownChunkSize(chunkSize, sessionChunkGranularity)
		if chunkSize < sessionMinChunkSize {
			chunkSize = sessionMinChunkSize
		}
	}

	if max <= 0 {
		max = DefaultSessionMaxParallelism
	}
	if max < DefaultSessionInitialParallelism {
		max = DefaultSessionInitialParallelism
	}
	if max > DefaultSessionMaxParallelism {
		max = DefaultSessionMaxParallelism
	}

	if initial <= 0 {
		initial = DefaultSessionInitialParallelism
	}
	if initial < DefaultSessionInitialParallelism {
		initial = DefaultSessionInitialParallelism
	}
	if initial > max {
		initial = max
	}

	return chunkSize, initial, max
}

func RecommendedSessionParallelism(fileSize int64) (initial int, max int) {
	max = runtime.GOMAXPROCS(0) * 2
	if max < 8 {
		max = 8
	}
	if max > DefaultSessionMaxParallelism {
		max = DefaultSessionMaxParallelism
	}

	partCount := 1
	if fileSize > 0 {
		partCount = int((fileSize + DefaultSessionChunkSize - 1) / DefaultSessionChunkSize)
	}

	initial = int(math.Ceil(math.Sqrt(float64(partCount)) * 2))
	if partCount >= 4 && initial < 4 {
		initial = 4
	}
	if initial < DefaultSessionInitialParallelism {
		initial = DefaultSessionInitialParallelism
	}
	if initial > max {
		initial = max
	}
	return initial, max
}

func RecommendedSessionChunkSize(fileSize int64, initial int) int64 {
	if initial <= 0 {
		initial = DefaultSessionInitialParallelism
	}
	if fileSize <= 0 {
		return sessionMinChunkSize
	}

	targetParts := int64(initial * sessionTargetChunkWaves)
	if targetParts <= 0 {
		targetParts = int64(DefaultSessionInitialParallelism * sessionTargetChunkWaves)
	}

	chunkSize := fileSize / targetParts
	if chunkSize < sessionMinChunkSize {
		chunkSize = sessionMinChunkSize
	}
	if chunkSize > DefaultSessionChunkSize {
		chunkSize = DefaultSessionChunkSize
	}
	if chunkSize > sessionMinChunkSize {
		chunkSize = roundDownChunkSize(chunkSize, sessionChunkGranularity)
		if chunkSize < sessionMinChunkSize {
			chunkSize = sessionMinChunkSize
		}
	}
	return chunkSize
}

func SuggestedSessionParallelismFloor(initial int) int {
	if initial <= DefaultSessionInitialParallelism {
		return DefaultSessionInitialParallelism
	}

	floor := (initial + 1) / 2
	if floor < 4 {
		floor = 4
	}
	if floor > initial {
		floor = initial
	}
	return floor
}

func SessionPolicySupportsFastPath(version string) bool {
	return strings.EqualFold(strings.TrimSpace(version), SessionAdaptivePolicyVersion)
}

func roundDownChunkSize(value int64, granularity int64) int64 {
	if value <= 0 || granularity <= 0 {
		return value
	}
	return (value / granularity) * granularity
}
