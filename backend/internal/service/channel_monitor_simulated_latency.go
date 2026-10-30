package service

import (
	"encoding/binary"
	"hash/fnv"
	"math"
	"time"
)

// Simulated monitors never send a request, so their stored history carries a
// zero first-token latency. The user-facing views replace it with a plausible
// value in this range; admin views keep the stored zero so the simulation stays
// visible to operators.
const (
	simulatedFirstTokenMinMs = 300
	simulatedFirstTokenMaxMs = 3000

	// Each monitor/model pair gets a typical speed in this band, and every check
	// varies around it by up to simulatedFirstTokenSpread in either direction.
	// 500 * 0.7 and 2300 * 1.3 both stay inside the min/max range above.
	simulatedFirstTokenBaseMinMs = 500
	simulatedFirstTokenBaseMaxMs = 2300
	simulatedFirstTokenSpread    = 0.3
)

// simulatedFirstTokenBaseMs is the typical first-token latency a simulated
// monitor/model pair reports; it doubles as the simulated 7-day average.
func simulatedFirstTokenBaseMs(monitorID int64, model string) int {
	unit := simulatedLatencyUnit(monitorID, model, 0, false)
	span := float64(simulatedFirstTokenBaseMaxMs - simulatedFirstTokenBaseMinMs)
	return simulatedFirstTokenBaseMinMs + int(math.Round(unit*span))
}

// simulatedFirstTokenLatencyMs returns the first-token latency shown for one
// simulated check. It is derived from the check time, so a history point reads
// the same on every request and the value only moves when a new check lands.
// A zero checkedAt (no check recorded yet) yields the pair's typical latency.
func simulatedFirstTokenLatencyMs(monitorID int64, model string, checkedAt time.Time) int {
	base := float64(simulatedFirstTokenBaseMs(monitorID, model))
	if checkedAt.IsZero() {
		return clampSimulatedFirstTokenMs(int(math.Round(base)))
	}
	unit := simulatedLatencyUnit(monitorID, model, checkedAt.UnixMilli(), true)
	factor := 1 + (unit*2-1)*simulatedFirstTokenSpread
	return clampSimulatedFirstTokenMs(int(math.Round(base * factor)))
}

func clampSimulatedFirstTokenMs(ms int) int {
	if ms < simulatedFirstTokenMinMs {
		return simulatedFirstTokenMinMs
	}
	if ms > simulatedFirstTokenMaxMs {
		return simulatedFirstTokenMaxMs
	}
	return ms
}

// simulatedLatencyUnit hashes the inputs into a stable value in [0, 1).
func simulatedLatencyUnit(monitorID int64, model string, stamp int64, perCheck bool) float64 {
	h := fnv.New64a()
	var buf [8]byte
	binary.LittleEndian.PutUint64(buf[:], uint64(monitorID))
	_, _ = h.Write(buf[:])
	_, _ = h.Write([]byte(model))
	if perCheck {
		_, _ = h.Write([]byte{0xff})
		binary.LittleEndian.PutUint64(buf[:], uint64(stamp))
		_, _ = h.Write(buf[:])
	}
	return float64(h.Sum64()>>11) / float64(uint64(1)<<53)
}

// simulatedLatencyReferenceTime picks the check a simulated summary describes:
// the latest primary check when there is one, otherwise the newest timeline
// point, otherwise the monitor's last run. Zero means it has never run.
func simulatedLatencyReferenceTime(
	m *ChannelMonitor,
	primaryLatest *ChannelMonitorLatest,
	timelineEntries []*ChannelMonitorHistoryEntry,
) time.Time {
	if primaryLatest != nil && !primaryLatest.CheckedAt.IsZero() {
		return primaryLatest.CheckedAt
	}
	for _, e := range timelineEntries {
		if e != nil && !e.CheckedAt.IsZero() {
			return e.CheckedAt
		}
	}
	if m.LastCheckedAt != nil {
		return *m.LastCheckedAt
	}
	return time.Time{}
}

// applySimulatedFirstTokenLatency fills the user view of a simulated monitor
// with plausible first-token latencies: the summary describes the check at
// referenceTime, and each timeline point recorded by a simulated check gets its
// own value. Real points from before the simulation keep what they measured.
// New pointers are assigned so stored history entries are never modified.
func applySimulatedFirstTokenLatency(
	m *ChannelMonitor,
	summary *MonitorStatusSummary,
	timeline []UserMonitorTimelinePoint,
	referenceTime time.Time,
) {
	primary := simulatedFirstTokenLatencyMs(m.ID, m.PrimaryModel, referenceTime)
	summary.PrimaryLatencyMs = &primary
	for i := range summary.ExtraModels {
		latency := simulatedFirstTokenLatencyMs(m.ID, summary.ExtraModels[i].Model, referenceTime)
		summary.ExtraModels[i].LatencyMs = &latency
	}
	for i := range timeline {
		if !isSimulatedCheckPoint(timeline[i].Status, timeline[i].LatencyMs) {
			continue
		}
		latency := simulatedFirstTokenLatencyMs(m.ID, m.PrimaryModel, timeline[i].CheckedAt)
		timeline[i].LatencyMs = &latency
	}
}

// isSimulatedCheckPoint reports whether a stored point came from a simulated
// check: a successful result without a measured latency. Real checks always
// measure a non-zero first-token latency, and failures keep their own values.
func isSimulatedCheckPoint(status string, latencyMs *int) bool {
	return status == MonitorStatusOperational && (latencyMs == nil || *latencyMs == 0)
}
