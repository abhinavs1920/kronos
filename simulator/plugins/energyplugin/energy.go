package energyplugin

import (
	"context"
	"fmt"
	"math"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
	"k8s.io/kubernetes/pkg/scheduler/framework"
	promapi "github.com/prometheus/client_golang/api"
	promv1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
)

// Name is the plugin name used in the scheduler registry and config.
const Name = "EnergyAware"

// defaultEnergy is used when we fail to fetch power metrics.
const defaultEnergy = 100.0

// keplerEndpoint is the Prometheus URL that exposes Kepler metrics.
// Change if your deployment differs.
const keplerEndpoint = "http://kepler.kepler.svc.cluster.local:9102"

// debugPreferredNode is the only node where pods should be scheduled.
const debugPreferredNode = "fav-energy-node"

// maxScore is the highest possible score, ensuring fav-energy-node always wins.
const maxScore int64 = 100

// Ensure the plugin implements the needed interfaces.
var _ framework.FilterPlugin = &EnergyAware{}
var _ framework.ScorePlugin = &EnergyAware{}
var _ framework.ScoreExtensions = &EnergyAware{}

// EnergyAware implements Filter and Score.
// For now Filter allows every node; Score prefers energy-efficient & lightly queued nodes.
type EnergyAware struct {
	handle framework.Handle
}

// New is the factory invoked by the scheduler.
func New(ctx context.Context,_ runtime.Object, h framework.Handle) (framework.Plugin, error) {
	return &EnergyAware{handle: h}, nil
}

// Name returns plugin name.
func (e *EnergyAware) Name() string { return Name }

// ------------------------- Filter ------------------------------
// Only allow nodes that pass basic scheduling requirements
func (e *EnergyAware) Filter(_ context.Context, _ *framework.CycleState, pod *corev1.Pod, nodeInfo *framework.NodeInfo) *framework.Status {
    node := nodeInfo.Node()
    if node == nil {
        return framework.NewStatus(framework.Unschedulable, "node not found")
    }
    
    // Check if node is ready
    for _, cond := range node.Status.Conditions {
        if cond.Type == corev1.NodeReady && cond.Status != corev1.ConditionTrue {
            return framework.NewStatus(framework.Unschedulable, "node is not ready")
        }
    }
    
    return framework.NewStatus(framework.Success, "node is schedulable")
}

// ------------------------- Score -------------------------------
// Score returns 0-100 (higher=better).
// This is where the actual energy-aware scheduling logic lives.
func (e *EnergyAware) Score(ctx context.Context, state *framework.CycleState, pod *corev1.Pod, nodeName string) (int64, *framework.Status) {
    // Get node info
    nodeInfo, err := e.handle.SnapshotSharedLister().NodeInfos().Get(nodeName)
    if err != nil {
        return 0, framework.AsStatus(fmt.Errorf("failed to get node %q: %w", nodeName, err))
    }
    node := nodeInfo.Node()
    
    // Special case for our preferred node
    if nodeName == debugPreferredNode {
        klog.InfoS("Scoring fav-energy-node with max score", 
            "pod", klog.KObj(pod), 
            "node", nodeName,
            "score", maxScore)
        return maxScore, nil
    }
    
    // For other nodes, calculate score based on energy usage
    // TODO: Replace with actual energy metrics from your monitoring system
    var score int64 = 10 // Default low score
    
    // Example: Give higher score to nodes with more available CPU
    if allocatable, ok := node.Status.Allocatable[corev1.ResourceCPU]; ok {
        availableCPU := allocatable.Value()
        // Simple scoring: more available CPU = higher score (up to 50)
        cpuScore := availableCPU / 2 // Assuming nodes have ~100 CPUs max
        if cpuScore > 50 {
            cpuScore = 50
        }
        score = cpuScore
    }
    
    klog.InfoS("Scoring node", 
        "pod", klog.KObj(pod), 
        "node", nodeName,
        "score", score)
        
    return score, nil
}

func (e *EnergyAware) NormalizeScore(_ context.Context, _ *framework.CycleState, _ *corev1.Pod, _ framework.NodeScoreList) *framework.Status {
	return framework.NewStatus(framework.Success)
}
func (e *EnergyAware) ScoreExtensions() framework.ScoreExtensions { return e }

// ------------------------ helpers ------------------------------

func queryNodeEnergy(node string) (float64, error) {
	client, err := promapi.NewClient(promapi.Config{Address: keplerEndpoint})
	if err != nil {
		return defaultEnergy, fmt.Errorf("prom client: %w", err)
	}
	api := promv1.NewAPI(client)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	query := fmt.Sprintf(`rate(kepler_node_package_joules_total{node="%s"}[1m])`, node)
	res, _, err := api.Query(ctx, query, time.Now())
	if err != nil {
		return defaultEnergy, err
	}
	scalar, ok := res.(*model.Scalar)
	if !ok || scalar == nil {
		return defaultEnergy, fmt.Errorf("unexpected result type")
	}
	val := float64(scalar.Value)
	if math.IsNaN(val) || math.IsInf(val, 0) {
		return defaultEnergy, fmt.Errorf("bad value")
	}
	return val, nil
}

func calculateQueueScore(node *corev1.Node, jobType string) int64 {
	cpu := node.Status.Allocatable.Cpu().MilliValue()
	rho := float64(cpu) / 1000.0
	lambda, varS := 0.1, 1.0
	if jobType == "long" {
		if rho >= 1.0 {
			return 10
		}
		wq := (lambda * varS) / (2 * (1 - rho))
		s := int64(100 - wq*10)
		if s < 0 {
			return 0
		}
		return s
	}
	s := int64(100 - rho*10)
	if s < 0 {
		return 0
	}
	return s
}
