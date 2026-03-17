package store

import "testing"

func TestEvaluateThresholds(t *testing.T) {
	ptr := func(f float64) *float64 { return &f }

	tests := []struct {
		name       string
		metrics    *HealthMetrics
		wantAlerts int
		wantMetric string // expected metric name of first alert, if any
	}{
		{
			name: "all healthy",
			metrics: &HealthMetrics{
				ConfidenceRecent7d:  ptr(0.85),
				ConfidenceAllTime:   ptr(0.87),
				StuckSessionCount:   0,
				OrphanedTriageCount: 0,
			},
			wantAlerts: 0,
		},
		{
			name: "confidence degraded",
			metrics: &HealthMetrics{
				ConfidenceRecent7d:  ptr(0.60),
				ConfidenceAllTime:   ptr(0.85),
				StuckSessionCount:   0,
				OrphanedTriageCount: 0,
			},
			wantAlerts: 1,
			wantMetric: "confidence_degradation",
		},
		{
			name: "confidence at exact threshold boundary",
			metrics: &HealthMetrics{
				ConfidenceRecent7d:  ptr(0.68), // exactly 80% of 0.85
				ConfidenceAllTime:   ptr(0.85),
				StuckSessionCount:   0,
				OrphanedTriageCount: 0,
			},
			wantAlerts: 0,
		},
		{
			name: "confidence just below threshold",
			metrics: &HealthMetrics{
				ConfidenceRecent7d:  ptr(0.679),
				ConfidenceAllTime:   ptr(0.85),
				StuckSessionCount:   0,
				OrphanedTriageCount: 0,
			},
			wantAlerts: 1,
			wantMetric: "confidence_degradation",
		},
		{
			name: "nil confidence scores",
			metrics: &HealthMetrics{
				ConfidenceRecent7d:  nil,
				ConfidenceAllTime:   nil,
				StuckSessionCount:   0,
				OrphanedTriageCount: 0,
			},
			wantAlerts: 0,
		},
		{
			name: "zero all-time confidence",
			metrics: &HealthMetrics{
				ConfidenceRecent7d:  ptr(0.5),
				ConfidenceAllTime:   ptr(0),
				StuckSessionCount:   0,
				OrphanedTriageCount: 0,
			},
			wantAlerts: 0,
		},
		{
			name: "stuck sessions at threshold",
			metrics: &HealthMetrics{
				ConfidenceRecent7d:  ptr(0.85),
				ConfidenceAllTime:   ptr(0.85),
				StuckSessionCount:   2,
				OrphanedTriageCount: 0,
			},
			wantAlerts: 0,
		},
		{
			name: "stuck sessions above threshold",
			metrics: &HealthMetrics{
				ConfidenceRecent7d:  ptr(0.85),
				ConfidenceAllTime:   ptr(0.85),
				StuckSessionCount:   3,
				OrphanedTriageCount: 0,
			},
			wantAlerts: 1,
			wantMetric: "stuck_sessions",
		},
		{
			name: "orphaned triage at threshold",
			metrics: &HealthMetrics{
				ConfidenceRecent7d:  ptr(0.85),
				ConfidenceAllTime:   ptr(0.85),
				StuckSessionCount:   0,
				OrphanedTriageCount: 3,
			},
			wantAlerts: 0,
		},
		{
			name: "orphaned triage above threshold",
			metrics: &HealthMetrics{
				ConfidenceRecent7d:  ptr(0.85),
				ConfidenceAllTime:   ptr(0.85),
				StuckSessionCount:   0,
				OrphanedTriageCount: 4,
			},
			wantAlerts: 1,
			wantMetric: "orphaned_triage",
		},
		{
			name: "multiple alerts",
			metrics: &HealthMetrics{
				ConfidenceRecent7d:  ptr(0.50),
				ConfidenceAllTime:   ptr(0.85),
				StuckSessionCount:   5,
				OrphanedTriageCount: 10,
			},
			wantAlerts: 3,
		},
		{
			name: "only recent confidence nil",
			metrics: &HealthMetrics{
				ConfidenceRecent7d:  nil,
				ConfidenceAllTime:   ptr(0.85),
				StuckSessionCount:   0,
				OrphanedTriageCount: 0,
			},
			wantAlerts: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			alerts := EvaluateThresholds(tt.metrics)
			if len(alerts) != tt.wantAlerts {
				t.Fatalf("EvaluateThresholds() returned %d alerts, want %d: %+v", len(alerts), tt.wantAlerts, alerts)
			}
			if tt.wantMetric != "" && len(alerts) > 0 && alerts[0].Metric != tt.wantMetric {
				t.Errorf("first alert metric = %q, want %q", alerts[0].Metric, tt.wantMetric)
			}
		})
	}
}
