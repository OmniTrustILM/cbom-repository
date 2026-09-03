package service_test

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"testing"
	"time"

	"github.com/OmniTrustILM/cbom-repository/internal/service"
	"github.com/OmniTrustILM/cbom-repository/internal/store"

	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/stretchr/testify/require"
)

func newSvc(t *testing.T, s3 *fakeS3) service.Service {
	t.Helper()
	svc, err := service.New(store.New(store.Config{Bucket: "bucket"}, s3, nil), service.Config{})
	require.NoError(t, err)
	return svc
}

func entryID(e service.SearchRes) string { return e.SerialNumber + "-" + e.Version }

func createdAtUnix(t *testing.T, e service.SearchRes) int64 {
	t.Helper()
	ts, err := time.Parse(time.RFC3339, e.Timestamp)
	require.NoError(t, err)
	return ts.Unix()
}

// iterate runs the documented client protocol once: fetch pages of `limit`, advance
// `after` to the last entry's created_at, stop at the first page shorter than `limit`.
// It returns every entry yielded and the page sizes, and asserts that consecutive pages
// never share a second (the server never splits a second).
func iterate(t *testing.T, svc service.Service, after int64, limit int) ([]service.SearchRes, []int) {
	t.Helper()
	var all []service.SearchRes
	var sizes []int
	for {
		page, err := svc.Search(context.Background(), after, limit)
		require.NoError(t, err)
		sizes = append(sizes, len(page))
		for i := 1; i < len(page); i++ {
			require.LessOrEqual(t, createdAtUnix(t, page[i-1]), createdAtUnix(t, page[i]), "page must be sorted by created_at")
		}
		if len(page) > 0 {
			require.Greater(t, createdAtUnix(t, page[0]), after, "entries must be strictly after `after`")
		}
		all = append(all, page...)
		if len(page) < limit {
			return all, sizes
		}
		after = createdAtUnix(t, page[len(page)-1])
	}
}

func idSet(entries []service.SearchRes) map[string]int {
	set := map[string]int{}
	for _, e := range entries {
		set[entryID(e)]++
	}
	return set
}

// assertExactlyOnce fails the test if any entry in `entries` was yielded more than once
// — within a single iteration of the documented protocol, every object must be yielded
// exactly once.
func assertExactlyOnce(t *testing.T, entries []service.SearchRes) {
	t.Helper()
	for id, n := range idSet(entries) {
		require.Equal(t, 1, n, "%s yielded %d times within one iteration", id, n)
	}
}

// assertEveryObjectYielded fails the test if any object currently in `s3` is missing
// from `entries` — used to confirm at-least-once delivery across one or more runs.
func assertEveryObjectYielded(t *testing.T, s3 *fakeS3, entries []service.SearchRes) {
	t.Helper()
	seen := idSet(entries)
	for key := range s3.objects {
		require.GreaterOrEqual(t, seen[key], 1, "missing %s", key)
	}
}

// injectLateUploads returns a fakeS3.beforeListing hook that, for listings 2 through 5,
// seeds three more objects landing between +30s and +45s of base — simulating uploads
// that complete while a run is already in progress.
func injectLateUploads(s3 *fakeS3, rng *rand.Rand, base time.Time) func(listing int) {
	return func(listing int) {
		if listing > 1 && listing < 6 {
			for j := 0; j < 3; j++ {
				s3.putWithStats(fmt.Sprintf("urn:uuid:late-%02d-%d-1", listing, j), base.Add(time.Duration(30+rng.Intn(15))*time.Second))
			}
		}
	}
}

// Deterministic walk through the soft-limit rules with concurrent uploads landing
// between pages. Seconds after base: 5 objects at +0, 3 at +1, 1 at +2, 8 at +3
// (some with millisecond offsets, as MinIO reports them), 8 at +10. limit = 4.
func TestSearch_Paged_ProtocolWithConcurrentUploads(t *testing.T) {
	base := time.Date(2026, 9, 2, 10, 0, 0, 0, time.UTC)
	s3 := newFakeS3(7) // small LIST pages so pagination is exercised
	seed := map[string]time.Duration{}
	add := func(n int, offset time.Duration, label string) {
		for i := 0; i < n; i++ {
			d := offset
			if i%2 == 1 {
				d += time.Duration(100*(i+1)) * time.Millisecond
			}
			key := fmt.Sprintf("urn:uuid:%s-%02d-1", label, i)
			seed[key] = d
			s3.putWithStats(key, base.Add(d))
		}
	}
	add(5, 0, "s00")
	add(3, 1*time.Second, "s01")
	add(1, 2*time.Second, "s02")
	add(8, 3*time.Second, "s03")
	add(8, 10*time.Second, "s10")

	// Listing 1 builds page 1 (boundary second +0). Before listing 2 two uploads land:
	// one late into the boundary second (+0.9 s) and one into a later second (+7 s).
	s3.beforeListing = func(listing int) {
		if listing == 2 {
			s3.putWithStats("urn:uuid:late-boundary-1", base.Add(900*time.Millisecond))
			s3.putWithStats("urn:uuid:late-later-1", base.Add(7*time.Second))
		}
	}
	svc := newSvc(t, s3)

	run1, sizes := iterate(t, svc, base.Unix()-1, 4)
	// page 1: limit hit at the 4th object of second +0 → the second is completed → 5
	// page 2: 3 (+1) + 1 (+2) = 4; next candidate is +3 → stop at exactly 4
	// page 3: second +3 has 8 → 8
	// page 4: late-later (+7) + 3 of +10 reach the limit → second +10 completed → 9
	// page 5: empty → done
	require.Equal(t, []int{5, 4, 8, 9, 0}, sizes)

	// Paged mode must not HEAD the whole bucket on every page: it HEADs exactly the
	// candidates it walks (here, one per yielded entry, since nothing vanishes in this
	// scenario), not every object in the bucket on every one of the 5 listings.
	headCallsInRun1 := s3.headCallCount()
	require.Equal(t, len(run1), headCallsInRun1, "every HEAD call in run 1 corresponds to a yielded entry (this scenario has no vanished objects)")
	require.Less(t, headCallsInRun1, len(sizes)*len(s3.objects), "paged mode must not HEAD the whole bucket on every listing")

	seen := idSet(run1)
	for key := range seed {
		require.Equal(t, 1, seen[key], "seeded object %s must be yielded exactly once within one iteration", key)
	}
	require.Equal(t, 1, seen["urn:uuid:late-later-1"], "an upload into a later second is yielded by the same iteration")
	require.Equal(t, 0, seen["urn:uuid:late-boundary-1"], "an upload into an already-passed second is deferred to the next iteration")

	// The pitfall the documented protocol avoids: a consumer that advanced its watermark
	// from the last created_at (minus a small overlap) would never see the late upload,
	// because it landed in a second the run had already passed.
	last := createdAtUnix(t, run1[len(run1)-1])
	naive, _ := iterate(t, svc, last-1, 4)
	require.Equal(t, 0, idSet(naive)["urn:uuid:late-boundary-1"], "advancing from the last created_at loses uploads that landed behind the cursor")

	// The documented rule: the next run starts from (run start − overlap). Run 1
	// conceptually started at base+10.5 s (after every seeded object existed); the late
	// upload is a multipart upload initiated at +0.9 s that completed during the run, so
	// the overlap must cover the longest upload duration — Core's 60 s does.
	runStart := base.Add(10500 * time.Millisecond)
	run2, _ := iterate(t, svc, runStart.Unix()-60, 4)
	require.Equal(t, 1, idSet(run2)["urn:uuid:late-boundary-1"])
	assertEveryObjectYielded(t, s3, append(run1, run2...))
}

// Randomised: many same-second objects, random limit, uploads injected during the
// first iteration. Within an iteration every object present when its page was built
// is yielded exactly once; a second iteration started from (run start − overlap)
// yields every object at least once.
func TestSearch_Paged_AtLeastOnceRandomised(t *testing.T) {
	base := time.Date(2026, 9, 2, 10, 0, 0, 0, time.UTC)
	for seed := int64(1); seed <= 5; seed++ {
		t.Run(fmt.Sprintf("seed-%d", seed), func(t *testing.T) {
			rng := rand.New(rand.NewSource(seed))
			s3 := newFakeS3(1000)
			for i := 0; i < 300; i++ {
				s3.putWithStats(fmt.Sprintf("urn:uuid:obj-%03d-1", i), base.Add(time.Duration(rng.Intn(40))*time.Second+time.Duration(rng.Intn(1000))*time.Millisecond))
			}
			limit := 1 + rng.Intn(12)
			s3.beforeListing = injectLateUploads(s3, rng, base)
			svc := newSvc(t, s3)

			run1, _ := iterate(t, svc, base.Unix()-1, limit)
			assertExactlyOnce(t, run1)
			s3.beforeListing = nil
			// Run 1 conceptually started right after the seeded objects existed (base+40 s);
			// the injected uploads have LastModified between +30 s and +45 s, so an overlap
			// of 60 s from the run start covers all of them.
			runStart := base.Add(40 * time.Second)
			run2, _ := iterate(t, svc, runStart.Unix()-60, limit)
			assertEveryObjectYielded(t, s3, append(run1, run2...))
		})
	}
}

// The boundary is evaluated at second granularity: an object whose LastModified is
// `after` plus 400 ms belongs to the `after` second and is excluded in paged mode (the
// legacy nanosecond compare would include it — that is what makes a consumer advancing
// by whole seconds safe on MinIO as well as on AWS).
func TestSearch_Paged_SecondGranularityBoundary(t *testing.T) {
	base := time.Date(2026, 9, 2, 10, 0, 0, 0, time.UTC)
	s3 := newFakeS3(1000)
	s3.putWithStats("urn:uuid:same-second-1", base.Add(400*time.Millisecond))
	s3.putWithStats("urn:uuid:next-second-1", base.Add(1*time.Second))
	svc := newSvc(t, s3)

	paged, err := svc.Search(context.Background(), base.Unix(), 10)
	require.NoError(t, err)
	require.Len(t, paged, 1)
	require.Equal(t, "urn:uuid:next-second", paged[0].SerialNumber)

	legacy, err := svc.Search(context.Background(), base.Unix(), 0)
	require.NoError(t, err)
	require.Len(t, legacy, 2, "legacy mode keeps the nanosecond compare")
}

func TestSearch_Paged_SortedByLastModifiedThenKey(t *testing.T) {
	base := time.Date(2026, 9, 2, 10, 0, 0, 0, time.UTC)
	s3 := newFakeS3(1000)
	s3.putWithStats("urn:uuid:c-1", base.Add(1*time.Second))
	s3.putWithStats("urn:uuid:a-1", base.Add(2*time.Second))
	s3.putWithStats("urn:uuid:b-1", base.Add(1*time.Second))
	s3.putWithStats("urn:uuid:d-original", base.Add(1*time.Second))
	svc := newSvc(t, s3)

	res, err := svc.Search(context.Background(), base.Unix(), 10)
	require.NoError(t, err)
	ids := make([]string, 0, len(res))
	for _, e := range res {
		ids = append(ids, entryID(e))
	}
	require.Equal(t, []string{"urn:uuid:b-1", "urn:uuid:c-1", "urn:uuid:d-original", "urn:uuid:a-1"}, ids)
}

// Vanished objects (deleted between LIST and HEAD) and foreign keys do not count
// toward the limit and do not fail the paged call.
func TestSearch_Paged_SkipsVanishedAndForeignKeys(t *testing.T) {
	base := time.Date(2026, 9, 2, 10, 0, 0, 0, time.UTC)
	s3 := newFakeS3(1000)
	s3.putWithStats("README", base.Add(1*time.Second)) // foreign object, no '-' in the key
	s3.putWithStats("urn:uuid:gone-1", base.Add(1*time.Second))
	s3.putWithStats("urn:uuid:a-1", base.Add(2*time.Second))
	s3.putWithStats("urn:uuid:b-1", base.Add(3*time.Second))
	// LIST still returns `gone`, but HEAD reports it deleted in the meantime.
	s3.headErr["urn:uuid:gone-1"] = &types.NotFound{}
	svc := newSvc(t, s3)

	res, err := svc.Search(context.Background(), base.Unix(), 2)
	require.NoError(t, err)
	require.Len(t, res, 2, "skipped objects do not count toward the limit")
	require.Equal(t, []string{"urn:uuid:a-1", "urn:uuid:b-1"}, []string{entryID(res[0]), entryID(res[1])})
}

// A skipped candidate (vanished or foreign-key) that sorts INSIDE the boundary second,
// after the page has already filled, must not clobber the boundary or count toward the
// limit: the page must still stop at the end of that second, and must not reopen past
// it.
func TestSearch_Paged_SkipInsideBoundarySecondDoesNotClobberBoundary(t *testing.T) {
	base := time.Date(2026, 9, 2, 10, 0, 0, 0, time.UTC)
	s3 := newFakeS3(1000)
	s3.putWithStats("urn:uuid:a-1", base.Add(5*time.Second))
	s3.putWithStats("urn:uuid:b-1", base.Add(5*time.Second))
	// Both sort after b-1 within the +5s second (see TestSearch_Paged_SortedByLastModifiedThenKey
	// for the sort rule): "gone" > "b" in the urn:uuid: namespace, and the dash-less
	// foreign key sorts last of all because it starts with 'z'.
	s3.putWithStats("urn:uuid:gone-1", base.Add(5*time.Second))
	s3.putWithStats("zzznodash", base.Add(5*time.Second)) // foreign object, no '-' in the key
	s3.putWithStats("urn:uuid:d-1", base.Add(6*time.Second))
	s3.headErr["urn:uuid:gone-1"] = &types.NotFound{}
	svc := newSvc(t, s3)

	res, err := svc.Search(context.Background(), base.Unix(), 2)
	require.NoError(t, err)
	require.Len(t, res, 2, "the vanished object and the foreign key inside the boundary second are skipped, not counted, and do not reopen the page into the +6s second")
	require.Equal(t, []string{"urn:uuid:a-1", "urn:uuid:b-1"}, []string{entryID(res[0]), entryID(res[1])})
}

// A HEAD failure other than not-found still fails the call (the page would be wrong).
func TestSearch_Paged_HeadErrorFailsCall(t *testing.T) {
	base := time.Date(2026, 9, 2, 10, 0, 0, 0, time.UTC)
	s3 := newFakeS3(1000)
	s3.putWithStats("urn:uuid:a-1", base.Add(1*time.Second))
	s3.headErr["urn:uuid:a-1"] = errors.New("boom")
	svc := newSvc(t, s3)

	_, err := svc.Search(context.Background(), base.Unix(), 5)
	require.Error(t, err)
}

func TestSearch_Paged_EmptyAndExactLimit(t *testing.T) {
	base := time.Date(2026, 9, 2, 10, 0, 0, 0, time.UTC)
	s3 := newFakeS3(1000)
	svc := newSvc(t, s3)

	res, err := svc.Search(context.Background(), base.Unix(), 3)
	require.NoError(t, err)
	require.Equal(t, []service.SearchRes{}, res, "empty, non-nil slice encodes as []")

	s3.putWithStats("urn:uuid:a-1", base.Add(1*time.Second))
	s3.putWithStats("urn:uuid:b-1", base.Add(2*time.Second))
	s3.putWithStats("urn:uuid:c-1", base.Add(3*time.Second))
	s3.putWithStats("urn:uuid:d-1", base.Add(4*time.Second))
	res, err = svc.Search(context.Background(), base.Unix(), 3)
	require.NoError(t, err)
	require.Len(t, res, 3, "limit reached exactly at a second boundary → no overshoot")
	require.Equal(t, "urn:uuid:c", res[2].SerialNumber)
}

// created_at must be derived from the same LIST LastModified that decides the page
// boundary, not from HEAD's: HEAD can report a different second — here simulating an
// unguarded overwrite landing between LIST and HEAD, or a store whose HTTP-date rounds
// differently from its listing. If created_at instead followed HEAD, a consumer
// advancing `after` to object a's created_at (+3 s, HEAD's view) would outrun the
// boundary (+1 s, LIST's view) and skip b and c on the next page.
func TestSearch_Paged_CreatedAtFollowsListingClock(t *testing.T) {
	base := time.Date(2026, 9, 2, 10, 0, 0, 0, time.UTC)
	s3 := newFakeS3(1000)
	s3.putWithStats("urn:uuid:a-1", base.Add(1*time.Second))
	s3.putWithStats("urn:uuid:b-1", base.Add(2*time.Second))
	s3.putWithStats("urn:uuid:c-1", base.Add(3*time.Second))
	s3.putWithStats("urn:uuid:d-1", base.Add(4*time.Second))
	s3.headTime["urn:uuid:a-1"] = base.Add(3 * time.Second)
	svc := newSvc(t, s3)

	run, _ := iterate(t, svc, base.Unix(), 1)
	seen := idSet(run)
	for _, id := range []string{"urn:uuid:a-1", "urn:uuid:b-1", "urn:uuid:c-1", "urn:uuid:d-1"} {
		require.Equal(t, 1, seen[id], "%s must be yielded exactly once in the run", id)
	}
	require.Equal(t, base.Add(1*time.Second).Unix(), createdAtUnix(t, run[0]), "created_at must follow the LIST second, not HEAD's")
}

func TestMaxSearchLimit(t *testing.T) {
	require.Equal(t, 1000, service.MaxSearchLimit)
}
