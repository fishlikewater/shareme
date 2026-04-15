package transfer

import "testing"

func TestRecommendedSessionProfileKeepsEnoughChunksInFlight(t *testing.T) {
	fileSize := int64(64 << 20)

	chunkSize, initial, max := RecommendedSessionProfile(fileSize)
	if initial < DefaultSessionInitialParallelism {
		t.Fatalf("expected initial parallelism >= %d, got %d", DefaultSessionInitialParallelism, initial)
	}
	if max < initial {
		t.Fatalf("expected max parallelism >= initial, got initial=%d max=%d", initial, max)
	}
	if chunkSize >= DefaultSessionChunkSize {
		t.Fatalf("expected adaptive chunk size smaller than default %d, got %d", DefaultSessionChunkSize, chunkSize)
	}
	partCount := int((fileSize + chunkSize - 1) / chunkSize)
	if partCount < initial*8 {
		t.Fatalf("expected at least 8 waves of chunks, got partCount=%d initial=%d chunkSize=%d", partCount, initial, chunkSize)
	}
}

func TestClampSessionProfileBoundsRemoteSuggestions(t *testing.T) {
	chunkSize, initial, max := ClampSessionProfile(1, 999, 999)
	if chunkSize != sessionMinChunkSize {
		t.Fatalf("expected chunk size to clamp to %d, got %d", sessionMinChunkSize, chunkSize)
	}
	if initial != DefaultSessionMaxParallelism || max != DefaultSessionMaxParallelism {
		t.Fatalf("expected parallelism to clamp to local max %d, got initial=%d max=%d", DefaultSessionMaxParallelism, initial, max)
	}

	chunkSize, initial, max = ClampSessionProfile(DefaultSessionChunkSize*2, 0, 0)
	if chunkSize != DefaultSessionChunkSize {
		t.Fatalf("expected chunk size to clamp to default max %d, got %d", DefaultSessionChunkSize, chunkSize)
	}
	if initial != DefaultSessionInitialParallelism || max != DefaultSessionMaxParallelism {
		t.Fatalf(
			"expected zero suggestions to fall back to defaults initial=%d max=%d, got initial=%d max=%d",
			DefaultSessionInitialParallelism,
			DefaultSessionMaxParallelism,
			initial,
			max,
		)
	}
}

func TestSuggestedSessionParallelismFloorKeepsBackoffHeadroom(t *testing.T) {
	cases := []struct {
		initial int
		want    int
	}{
		{initial: 2, want: 2},
		{initial: 6, want: 4},
		{initial: 12, want: 6},
	}

	for _, testCase := range cases {
		if got := SuggestedSessionParallelismFloor(testCase.initial); got != testCase.want {
			t.Fatalf("expected floor %d for initial %d, got %d", testCase.want, testCase.initial, got)
		}
	}
}

func TestSessionPolicySupportsFastPathRequiresExactVersion(t *testing.T) {
	if !SessionPolicySupportsFastPath(SessionAdaptivePolicyVersion) {
		t.Fatalf("expected %q to enable fast path", SessionAdaptivePolicyVersion)
	}
	if SessionPolicySupportsFastPath("v2-next-experiment") {
		t.Fatal("expected unrelated v2 variant to keep default safety path")
	}
}
