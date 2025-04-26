package aztec

import (
	"fmt"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"go.uber.org/zap"
)

// Global metrics variables
var (
	messagesConfirmedMetric *prometheus.CounterVec
	messagesPendingMetric   *prometheus.GaugeVec
	finalityTimeMetric      *prometheus.HistogramVec
	lookupFailuresMetric    *prometheus.CounterVec
	metricsInitialized      sync.Once
)

// initMetrics initializes the metrics only once
func initMetrics() {
	metricsInitialized.Do(func() {
		messagesConfirmedMetric = promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "wormhole_aztec_observations_confirmed_total",
				Help: "Total number of verified observations found for the chain",
			}, []string{"chain_name"})

		messagesPendingMetric = promauto.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "wormhole_aztec_observations_pending",
				Help: "Number of observations waiting for finality",
			}, []string{"chain_name"})

		finalityTimeMetric = promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "wormhole_aztec_finality_time_seconds",
				Help:    "Time in seconds for an Aztec block to be finalized",
				Buckets: prometheus.ExponentialBuckets(10, 2, 10), // From 10s to ~2.8h
			}, []string{"chain_name"})

		lookupFailuresMetric = promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "wormhole_aztec_lookup_failures_total",
				Help: "Number of failures when looking up Aztec blocks",
			}, []string{"chain_name"})
	})
}

// ObservationManager handles storage and lifecycle of pending observations
type ObservationManager interface {
	QueueObservation(params LogParameters, payload []byte, blockInfo BlockInfo, blockNumber int) string
	GetPendingObservations() map[string]*PendingObservation
	RemoveObservation(id string)
	UpdateMetrics()
	LogPendingObservations()
	GetObservationByID(id string) (*PendingObservation, bool)
	RecordFinalityTime(duration float64)
	IncrementLookupFailures()
	IncrementMessagesConfirmed()
}

// observationManager is the implementation of ObservationManager
type observationManager struct {
	networkID           string
	logger              *zap.Logger
	pendingObservations map[string]*PendingObservation
	mutex               sync.RWMutex
	metrics             observationMetrics
}

// observationMetrics holds the Prometheus metrics for the observation manager
type observationMetrics struct {
	messagesConfirmed *prometheus.CounterVec
	messagesPending   *prometheus.GaugeVec
	finalityTime      *prometheus.HistogramVec
	lookupFailures    *prometheus.CounterVec
}

// NewObservationManager creates a new observation manager
func NewObservationManager(networkID string, logger *zap.Logger) ObservationManager {
	logger.Info("Creating new observation manager", zap.String("networkID", networkID))

	// Initialize metrics if not already done
	initMetrics()

	// Use the global metrics
	metrics := observationMetrics{
		messagesConfirmed: messagesConfirmedMetric,
		messagesPending:   messagesPendingMetric,
		finalityTime:      finalityTimeMetric,
		lookupFailures:    lookupFailuresMetric,
	}

	logger.Info("Observation manager created with metrics initialized")

	return &observationManager{
		networkID:           networkID,
		logger:              logger,
		pendingObservations: make(map[string]*PendingObservation),
		metrics:             metrics,
	}
}

// QueueObservation adds a new observation to the pending queue
func (m *observationManager) QueueObservation(params LogParameters, payload []byte, blockInfo BlockInfo, blockNumber int) string {
	// Create a unique ID for this observation
	observationID := m.createObservationID(params, blockNumber)

	m.logger.Info("Creating new pending observation",
		zap.String("id", observationID),
		zap.Int("block", blockNumber),
		zap.Uint64("sequence", params.Sequence),
		zap.Stringer("emitter", params.SenderAddress))

	// Create pending observation entry
	pendingObservation := &PendingObservation{
		Params:        params,
		Payload:       payload,
		BlockInfo:     blockInfo,
		AztecBlockNum: blockNumber,
		AttemptCount:  0,
		SubmitTime:    time.Now(),
	}

	// Add to pending map
	m.mutex.Lock()
	defer m.mutex.Unlock()

	// Check if this observation already exists
	if existing, exists := m.pendingObservations[observationID]; exists {
		m.logger.Warn("Observation already pending, replacing",
			zap.String("id", observationID),
			zap.Int("existing_block", existing.AztecBlockNum),
			zap.Int("new_block", blockNumber))
	}

	m.pendingObservations[observationID] = pendingObservation
	pendingCount := len(m.pendingObservations)

	// Update metrics
	m.metrics.messagesPending.WithLabelValues(m.networkID).Set(float64(pendingCount))

	m.logger.Info("Queued observation for finality check",
		zap.String("id", observationID),
		zap.Int("aztec_block", blockNumber),
		zap.Uint64("sequence", params.Sequence),
		zap.Stringer("emitter", params.SenderAddress),
		zap.Int("total_pending", pendingCount))

	return observationID
}

// GetPendingObservations returns a copy of all pending observations
func (m *observationManager) GetPendingObservations() map[string]*PendingObservation {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	count := len(m.pendingObservations)
	m.logger.Info("Getting pending observations", zap.Int("count", count))

	// Create a copy to avoid concurrency issues
	result := make(map[string]*PendingObservation, count)
	for id, obs := range m.pendingObservations {
		result[id] = obs
	}

	return result
}

// RemoveObservation removes an observation from the pending queue
func (m *observationManager) RemoveObservation(id string) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	if obs, exists := m.pendingObservations[id]; exists {
		m.logger.Info("Removing observation from pending map",
			zap.String("id", id),
			zap.Int("aztec_block", obs.AztecBlockNum))
		delete(m.pendingObservations, id)
	} else {
		m.logger.Warn("Attempted to remove non-existent observation", zap.String("id", id))
	}
}

// GetObservationByID retrieves a specific observation by ID
func (m *observationManager) GetObservationByID(id string) (*PendingObservation, bool) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	obs, exists := m.pendingObservations[id]
	if exists {
		m.logger.Debug("Retrieved observation by ID", zap.String("id", id))
	} else {
		m.logger.Debug("Observation not found by ID", zap.String("id", id))
	}
	return obs, exists
}

// UpdateMetrics updates prometheus metrics based on current state
func (m *observationManager) UpdateMetrics() {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	pendingCount := len(m.pendingObservations)
	m.metrics.messagesPending.WithLabelValues(m.networkID).Set(float64(pendingCount))
	m.logger.Debug("Updated pending observations metric", zap.Int("count", pendingCount))
}

// LogPendingObservations logs detailed information about pending observations
func (m *observationManager) LogPendingObservations() {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	mapSize := len(m.pendingObservations)

	if mapSize == 0 {
		m.logger.Info("No pending observations")
		return
	}

	// Just log the count at info level, not all the details
	m.logger.Info("Pending observations", zap.Int("total_count", mapSize))

	// Log details at info level for debugging
	for id, obs := range m.pendingObservations {
		m.logger.Info("Pending observation details",
			zap.String("id", id),
			zap.Int("aztec_block", obs.AztecBlockNum),
			zap.Uint64("sequence", obs.Params.Sequence),
			zap.Stringer("sender", obs.Params.SenderAddress),
			zap.Duration("pending_for", time.Since(obs.SubmitTime)),
			zap.Int("attempts", obs.AttemptCount))
	}
}

// RecordFinalityTime records the time it took for an observation to be finalized
func (m *observationManager) RecordFinalityTime(duration float64) {
	m.metrics.finalityTime.WithLabelValues(m.networkID).Observe(duration)
	m.logger.Info("Recorded finality time", zap.Float64("duration_seconds", duration))
}

// IncrementLookupFailures increases the counter for lookup failures
func (m *observationManager) IncrementLookupFailures() {
	m.metrics.lookupFailures.WithLabelValues(m.networkID).Inc()
	m.logger.Info("Incremented lookup failures counter")
}

// IncrementMessagesConfirmed increases the counter for confirmed messages
func (m *observationManager) IncrementMessagesConfirmed() {
	m.metrics.messagesConfirmed.WithLabelValues(m.networkID).Inc()
	m.logger.Info("Incremented messages confirmed counter")
}

// createObservationID creates a unique ID for tracking pending observations
func (m *observationManager) createObservationID(params LogParameters, blockNumber int) string {
	id := fmt.Sprintf("%s-%d-%d", params.SenderAddress.String(), params.Sequence, blockNumber)
	m.logger.Debug("Created observation ID", zap.String("id", id))
	return id
}
