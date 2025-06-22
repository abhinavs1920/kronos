package main

import (
	"os"

	"k8s.io/component-base/cli"
	_ "k8s.io/component-base/logs/json/register" // for JSON log format registration
	_ "k8s.io/component-base/metrics/prometheus/clientgo"
	_ "k8s.io/component-base/metrics/prometheus/version" // for version metric registration
	"k8s.io/klog"
	"sigs.k8s.io/kube-scheduler-simulator/simulator/pkg/debuggablescheduler"
	"sigs.k8s.io/kube-scheduler-simulator/simulator/plugins/energyplugin"

)

func main() {
	klog.Info("Starting kube-scheduler-simulator with EnergyAware plugin")
	klog.Info("Registering EnergyAware plugin")
	
	command, cancelFn, err := debuggablescheduler.NewSchedulerCommand(
		debuggablescheduler.WithPlugin("EnergyAware", energyplugin.New), // Case-sensitive plugin name
	)
	if err != nil {
		klog.Error(err, "Failed to build the debuggablescheduler command")
		os.Exit(1)
	}

	klog.Info("Running scheduler command...")
	code := cli.Run(command)

	klog.Info("Scheduler command completed", "exitCode", code)
	cancelFn()
	os.Exit(code)
}
