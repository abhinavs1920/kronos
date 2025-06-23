package energyplugin

import (
	"context"
	"fmt"
	"hash/fnv"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
	"k8s.io/kubernetes/pkg/scheduler/framework"
	schedmetrics "sigs.k8s.io/kube-scheduler-simulator/simulator/plugins/kronos/metrics"
	kronos "sigs.k8s.io/kube-scheduler-simulator/simulator/plugins/kronos/models"
)

// Name is the plugin name used in the scheduler registry and config.
const Name = "EnergyAware"

// baseWatts is the baseline power usage in watts
const baseWatts = 50.0

// nodeVariance controls how much the power usage varies between nodes (as a percentage of baseWatts)
const nodeVariance = 0.5 // ±50% variance for more spread

// maxScore is the highest possible score returned by the plugin.
const maxScore int64 = 100

// Ensure the plugin implements the needed interfaces.
var _ framework.FilterPlugin = &EnergyAware{}
var _ framework.ScorePlugin = &EnergyAware{}
var _ framework.ScoreExtensions = &EnergyAware{}

// EnergyAware implements Filter and Score.
// For now Filter allows every node; Score prefers energy-efficient & lightly queued nodes.
type EnergyAware struct {
	handle  framework.Handle
	metrics *schedmetrics.MetricsCollector
}

var (
	logFile *os.File
	once    sync.Once

	// Queue statistics tracking
	statsMu   sync.Mutex
	nodeStats = make(map[string]*NodeQueueStats)
)

// ---------------- Queue tracking & parameters ------------------

type NodeQueueStats struct {
	Arrivals     []time.Time
	ServiceTimes []float64
}

const (
	historyWindow     = 10   // recent jobs to keep for λ/μ estimation
	delayPenaltyShort = 5.0  // weight applied to delay for short jobs
	delayPenaltyLong  = 15.0 // heavier penalty for long jobs
	energyWeight      = 0.1  // converts watts → score penalty
)

func recordArrival(nodeName string, serviceTime float64) {
	statsMu.Lock()
	defer statsMu.Unlock()

	s := nodeStats[nodeName]
	if s == nil {
		s = &NodeQueueStats{}
		nodeStats[nodeName] = s
	}
	now := time.Now()
	s.Arrivals = append(s.Arrivals, now)
	s.ServiceTimes = append(s.ServiceTimes, serviceTime)

	if len(s.Arrivals) > historyWindow {
		s.Arrivals = s.Arrivals[len(s.Arrivals)-historyWindow:]
		s.ServiceTimes = s.ServiceTimes[len(s.ServiceTimes)-historyWindow:]
	}
}

func computeRates(nodeName string) (lambda, mu, varS float64) {
	statsMu.Lock()
	defer statsMu.Unlock()

	s := nodeStats[nodeName]
	if s == nil || len(s.Arrivals) < 2 {
		return 0, 0, 0
	}

	duration := s.Arrivals[len(s.Arrivals)-1].Sub(s.Arrivals[0]).Seconds()
	if duration > 0 {
		lambda = float64(len(s.Arrivals)) / duration
	}

	// mean service time
	sum := 0.0
	for _, st := range s.ServiceTimes {
		sum += st
	}
	if len(s.ServiceTimes) > 0 {
		mean := sum / float64(len(s.ServiceTimes))
		if mean > 0 {
			mu = 1 / mean
		}
		// variance of service time
		varSum := 0.0
		for _, st := range s.ServiceTimes {
			diff := st - mean
			varSum += diff * diff
		}
		varS = varSum / float64(len(s.ServiceTimes))
	}
	return
}

// mockNodeWatts produces a deterministic pseudo-random power draw for a node.
func mockNodeWatts(nodeName string) float64 {
	hash := fnv.New32a()
	timestamp := time.Now().Unix() / 10 // refresh every 10s
	hash.Write([]byte(fmt.Sprintf("%s-%d", nodeName, timestamp)))
	nodeHash := hash.Sum32()

	jitter := float64(nodeHash%2000-1000) / 10000.0
	variance := 1 - nodeVariance + float64(nodeHash%uint32(2*nodeVariance*100))/100.0 + jitter
	watts := baseWatts * variance
	return math.Max(10, math.Min(100, watts))
}

// initLogging sets up file-based logging
func initLogging() error {
	var err error
	once.Do(func() {
		logDir := "/tmp/energy-scheduler-logs"
		if err := os.MkdirAll(logDir, 0755); err != nil {
			klog.ErrorS(err, "Failed to create log directory")
			return
		}

		logPath := filepath.Join(logDir, "energy-aware.log")
		logFile, err = os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			klog.ErrorS(err, "Failed to open log file")
			return
		}
		klog.InfoS("Logging to file", "path", logPath)
	})
	return err
}

// logToFile writes a message to both klog and the log file
func logToFile(level, msg string, keysAndValues ...interface{}) {
	logMsg := fmt.Sprintf("[%s] %s", level, msg)
	for i := 0; i < len(keysAndValues); i += 2 {
		if i+1 < len(keysAndValues) {
			logMsg += fmt.Sprintf(" %v=%v", keysAndValues[i], keysAndValues[i+1])
		}
	}
	logMsg += "\n"

	if logFile != nil {
		logFile.WriteString(fmt.Sprintf("[%s] %s", time.Now().Format(time.RFC3339), logMsg))
		logFile.Sync()
	}
}

// New is the factory invoked by the scheduler.
func New(ctx context.Context, _ runtime.Object, h framework.Handle) (framework.Plugin, error) {
	if err := initLogging(); err != nil {
		klog.ErrorS(err, "Failed to initialize logging")
	}

	klog.InfoS("Initializing EnergyAware plugin with deterministic scoring")

	// initialise metrics collector
	collector, err := schedmetrics.NewMetricsCollector("/tmp/kronos-metrics.json")
	if err != nil {
		klog.ErrorS(err, "Failed to initialize metrics collector")
	}

	return &EnergyAware{
		handle:  h,
		metrics: collector,
	}, nil
}

// Name returns plugin name.
func (e *EnergyAware) Name() string { return Name }

// ------------------------- Filter ------------------------------
// Only allow nodes that pass basic scheduling requirements
func (e *EnergyAware) Filter(_ context.Context, _ *framework.CycleState, pod *corev1.Pod, nodeInfo *framework.NodeInfo) *framework.Status {
	node := nodeInfo.Node()
	if node == nil {
		klog.InfoS("Filter: node not found in cache", "pod", klog.KObj(pod), "node", nodeInfo.Node().GetName())
		return framework.NewStatus(framework.Unschedulable, "node not found")
	}

	// Check if node is ready
	for _, cond := range node.Status.Conditions {
		if cond.Type == corev1.NodeReady && cond.Status != corev1.ConditionTrue {
			klog.InfoS("Filter: node not ready", "pod", klog.KObj(pod), "node", node.Name, "condition", cond)
			return framework.NewStatus(framework.Unschedulable, "node is not ready")
		}
	}

	logToFile("DEBUG", "Node passed checks", "pod", klog.KObj(pod).String(), "node", node.Name)
	klog.InfoS("Filter: node passed checks", "pod", klog.KObj(pod), "node", node.Name)
	return framework.NewStatus(framework.Success, "node is schedulable")
}

// ------------------------- Score -------------------------------
// Score returns 0-100 (higher = better). The lower the power draw (Watts) reported by
// Kepler, the higher the score. A simple linear mapping is used:
//
//	0 W  -> 100 points
//	500 W ->  50 points
//	>=1000 W -> 0 points
//
// Any query failure falls back to a default mid-range energy value.
func (e *EnergyAware) Score(ctx context.Context, _ *framework.CycleState, pod *corev1.Pod, nodeName string) (int64, *framework.Status) {
	// ---------- Job classification ----------
	jobType := "short"
	if jt, ok := pod.Labels["jobType"]; ok {
		jobType = jt
	}

	// ---------- Estimate service time (mock) ----------
	var serviceTime float64
	if jobType == "long" {
		serviceTime = rand.Float64()*2 + 5 // 5–7 s
	} else {
		serviceTime = rand.Float64()*0.5 + 1 // 1–1.5 s
	}

	// Record this arrival into moving window
	recordArrival(nodeName, serviceTime)

	// ---------- Compute λ, μ & Var(S) ----------
	lambda, mu, varS := computeRates(nodeName)

	// ---------- Queueing delay ----------
	var wq float64
	if jobType == "long" {
		wq = kronos.CalculateWqMG1(lambda, mu, varS)
	} else {
		wq = kronos.CalculateWqMM1(lambda, mu)
	}
	if math.IsInf(wq, 1) || math.IsNaN(wq) {
		wq = 1e6 // huge penalty for unstable nodes
	}

	// ---------- Energy cost ----------
	watts := mockNodeWatts(nodeName)

	// ---------- Final scoring ----------
	delayPenalty := delayPenaltyShort
	if jobType == "long" {
		delayPenalty = delayPenaltyLong
	}
	totalPenalty := wq*delayPenalty + watts*energyWeight

	scoreFloat := float64(maxScore) - totalPenalty
	if scoreFloat < 0 {
		scoreFloat = 0
	}
	finalScore := int64(math.Round(scoreFloat))

	logToFile("INFO", "NodeScore",
		"pod", klog.KObj(pod).String(),
		"node", nodeName,
		"jobType", jobType,
		"lambda", fmt.Sprintf("%.3f", lambda),
		"mu", fmt.Sprintf("%.3f", mu),
		"Wq", fmt.Sprintf("%.3f", wq),
		"watts", fmt.Sprintf("%.2f", watts),
		"score", finalScore,
	)

	// record metrics
	if e.metrics != nil {
		_ = e.metrics.Record(schedmetrics.SchedulerMetrics{
			PodName:     pod.Name,
			NodeName:    nodeName,
			JobType:     jobType,
			ArrivalRate: lambda,
			ServiceRate: mu,
			VarianceS:   varS,
			QueueDelay:  wq,
			EnergyUsage: watts,
			FinalScore:  finalScore,
			IsScheduled: true,
		})
	}

	return finalScore, framework.NewStatus(framework.Success)
}

/*
	        jobType = t
	    }

	    // ---------------- Queue score ------------------------
	    nodeInfo, err := e.handle.SnapshotSharedLister().NodeInfos().Get(nodeName)
	    if err != nil {
	        return 0, framework.AsStatus(fmt.Errorf("failed to get node info: %w", err))
	    }
	    queueScore := calculateQueueScore(nodeInfo, jobType)

	    // ---------------- Energy score -----------------------
	    // Generate a semi-random energy value based on node name and current time
	    // This ensures scores vary over time while maintaining some consistency per node
	    hash := fnv.New32a()
	    timestamp := time.Now().Unix() / 10 // Change every 10 seconds
	    hash.Write([]byte(fmt.Sprintf("%s-%d", nodeName, timestamp)))
	    nodeHash := hash.Sum32()

	    // Generate a value between (1-nodeVariance) and (1+nodeVariance)
	    // Add some jitter to make it more dynamic
	    jitter := float64(nodeHash%2000 - 1000) / 10000.0 // ±10% jitter
	    variance := 1 - nodeVariance + float64(nodeHash%uint32(2*nodeVariance*100))/100.0 + jitter
	    watts := baseWatts * variance

	    // Ensure watts stays within reasonable bounds
	    watts = math.Max(10, math.Min(100, watts)) // Keep between 10W and 100W

	    // Convert Watts → score. Lower watts = higher score.
	    energyNorm := math.Min(watts/10.0, 100.0) // Map 0-1000 W → 0-100
	    score := maxScore - int64(energyNorm)     // Invert so lower watts ⇒ higher score
	    if score < 0 {
	        score = 0
	    }

	    // ---------------- Combine scores --------------------
	    // Weigh energy 60% and queue 40%
	    finalScore := int64(math.Round(float64(score)*0.6 + float64(queueScore)*0.4))
	    if finalScore > maxScore {
	        finalScore = maxScore
	    }

	    logMsg := fmt.Sprintf("NodeScore pod=%s node=%s watts=%.2f energyScore=%d queueScore=%d final=%d",
	        klog.KObj(pod).String(), nodeName, watts, score, queueScore, finalScore)
	    logToFile("INFO", logMsg)

	    klog.InfoS("EnergyAware score",
	        "pod", klog.KObj(pod),
	        "node", nodeName,
	        "watts", fmt.Sprintf("%.2f", watts),
	        "energyScore", score,
	        "queueScore", queueScore,
	        "final", finalScore)
	    return finalScore, framework.NewStatus(framework.Success)
	}
*/
func (e *EnergyAware) NormalizeScore(_ context.Context, _ *framework.CycleState, _ *corev1.Pod, _ framework.NodeScoreList) *framework.Status {
	return framework.NewStatus(framework.Success)
}
func (e *EnergyAware) ScoreExtensions() framework.ScoreExtensions { return e }

// ------------------------ helpers ------------------------------

// calculateQueueScore returns a score (0-100) based on node queue length and job type.
// For short jobs (default): light penalty per queued pod.
// For long jobs: heavier penalty.
func calculateQueueScore(nodeInfo *framework.NodeInfo, jobType string) int64 {
	// Use the correct method to get pods on the node
	podsOnNode := nodeInfo.Pods
	queueLen := len(podsOnNode)

	// Parameters: how much each queued pod reduces score
	var penaltyPerPod int64 = 5 // short jobs
	if jobType == "long" {
		penaltyPerPod = 10 // long jobs are more sensitive
	}

	score := maxScore - int64(queueLen)*penaltyPerPod
	if score < 0 {
		score = 0
	}
	return score
}
