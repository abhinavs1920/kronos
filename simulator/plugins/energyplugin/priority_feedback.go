package energyplugin

import (
	"context"
	"hash/fnv"
	"math"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
	"k8s.io/kubernetes/pkg/scheduler/framework"
)

const (
	// PrioritySchedulerName is the name of the priority feedback scheduler
	PrioritySchedulerName = "PriorityFeedback"
	
	// StarvationThreshold is the time after which a pod is considered starving
	StarvationThreshold = 5 * time.Minute
	
	// PriorityBoost is the score boost given to starving pods
	PriorityBoost = 50.0
)

// PriorityFeedback implements a multi-level feedback queue scheduler
type PriorityFeedback struct {
	handle framework.Handle
	mu     sync.Mutex
	
	// podQueues tracks pods waiting in each priority level
	podQueues map[string]map[string]time.Time // pod.UID -> {nodeName: enqueueTime}
}

// NewPriorityFeedback creates a new PriorityFeedback scheduler
func NewPriorityFeedback(_ runtime.Object, h framework.Handle) (framework.Plugin, error) {
	return &PriorityFeedback{
		handle:    h,
		podQueues: make(map[string]map[string]time.Time),
	}, nil
}

// Name returns the plugin name
func (p *PriorityFeedback) Name() string {
	return PrioritySchedulerName
}

// Score calculates the score for a node based on job type and starvation
func (p *PriorityFeedback) Score(ctx context.Context, state *framework.CycleState, pod *corev1.Pod, nodeName string) (int64, *framework.Status) {
	// Get job type from pod labels (default to short)
	jobType := pod.Labels["jobType"]
	if jobType == "" {
		jobType = "short"
	}

	// Check if pod is starving
	isStarving := p.isPodStarving(pod, nodeName)

	// Get base energy score (0-100)
	energyScore := p.calculateEnergyScore(nodeName)

	// Calculate priority score based on job type and starvation
	priorityScore := p.calculatePriorityScore(jobType, isStarving)

	// Combine scores (60% energy, 40% priority)
	finalScore := int64(math.Round(float64(energyScore)*0.6 + float64(priorityScore)*0.4))

	klog.InfoS("PriorityFeedback score",
		"pod", klog.KObj(pod),
		"node", nodeName,
		"jobType", jobType,
		"starving", isStarving,
		"energy", energyScore,
		"priority", priorityScore,
		"final", finalScore)

	return finalScore, nil
}

// calculateEnergyScore returns a score based on node's energy usage
func (p *PriorityFeedback) calculateEnergyScore(nodeName string) int64 {
	// In a real implementation, this would query actual energy metrics
	// For now, using a simple hash-based approach similar to the main plugin
	hash := fnv.New32a()
	hash.Write([]byte(nodeName))
	nodeHash := hash.Sum32()
	
	// Generate a value between 10-90 based on node name
	// This simulates different energy usage patterns
	return int64(10 + (nodeHash % 80))
}

// calculatePriorityScore calculates the priority score based on job type and starvation
func (p *PriorityFeedback) calculatePriorityScore(jobType string, isStarving bool) int64 {
	switch {
	case isStarving:
		// Maximum priority for starving pods
		return 100
	case jobType == "long":
		// Long jobs get medium priority
		return 50
	default:
		// Short jobs get high priority
		return 80
	}
}

// isPodStarving checks if a pod has been waiting too long to be scheduled
func (p *PriorityFeedback) isPodStarving(pod *corev1.Pod, nodeName string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()

	now := time.Now()
	podID := string(pod.UID)

	// Initialize node map if it doesn't exist
	if p.podQueues[podID] == nil {
		p.podQueues[podID] = make(map[string]time.Time)
	}

	// Record or update the enqueue time for this pod on this node
	if _, exists := p.podQueues[podID][nodeName]; !exists {
		p.podQueues[podID][nodeName] = now
	}

	enqueueTime := p.podQueues[podID][nodeName]
	waitingTime := now.Sub(enqueueTime)

	// Consider pod starving if waiting longer than threshold
	return waitingTime > StarvationThreshold
}

// NormalizeScore normalizes the scores across all nodes
func (p *PriorityFeedback) NormalizeScore(ctx context.Context, state *framework.CycleState, pod *corev1.Pod, scores framework.NodeScoreList) *framework.Status {
	// Find the max score for normalization
	var maxScore int64 = 0
	for _, nodeScore := range scores {
		if nodeScore.Score > maxScore {
			maxScore = nodeScore.Score
		}
	}

	// Normalize scores to 0-100 range
	for i := range scores {
		if maxScore > 0 {
			scores[i].Score = scores[i].Score * framework.MaxNodeScore / maxScore
		}
	}

	return nil
}

// ScoreExtensions returns the ScoreExtensions interface
func (p *PriorityFeedback) ScoreExtensions() framework.ScoreExtensions {
	return p
}

// CleanupPod is called when a pod is being deleted
func (p *PriorityFeedback) CleanupPod(ctx context.Context, state *framework.CycleState, pod *corev1.Pod, nodeName string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Clean up pod from queues when it's scheduled
	podID := string(pod.UID)
	delete(p.podQueues, podID)
}
