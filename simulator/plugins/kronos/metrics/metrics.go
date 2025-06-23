package metrics

import (
    "encoding/json"
    "io"
    "os"
    "sync"
    "time"
)

// SchedulerMetrics captures per-decision metrics emitted by the scheduler.
// Fields are tagged for compact JSON output so that the file is easy to post-process.
type SchedulerMetrics struct {
    Timestamp    time.Time `json:"ts"`
    PodName      string    `json:"pod"`
    NodeName     string    `json:"node"`
    JobType      string    `json:"jobType"`
    ArrivalRate  float64   `json:"lambda"`
    ServiceRate  float64   `json:"mu"`
    VarianceS    float64   `json:"varS"`
    QueueDelay   float64   `json:"wq"`
    EnergyUsage  float64   `json:"watts"`
    FinalScore   int64     `json:"score"`
    IsScheduled  bool      `json:"scheduled"`
    ErrorMessage string    `json:"err,omitempty"`
}

// MetricsCollector writes SchedulerMetrics entries to a JSON array on disk.
type MetricsCollector struct {
    file      *os.File
    mu        sync.Mutex
    firstDone bool // whether the first record has been written
}

// NewMetricsCollector opens (or creates) the file at path and writes an opening '[' if the file is empty.
func NewMetricsCollector(path string) (*MetricsCollector, error) {
    f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0644)
    if err != nil {
        return nil, err
    }

    // Check if the file is empty; if so, write opening bracket.
    fi, err := f.Stat()
    if err != nil {
        _ = f.Close()
        return nil, err
    }

    if fi.Size() == 0 {
        if _, err := f.WriteString("[\n"); err != nil {
            _ = f.Close()
            return nil, err
        }
    } else {
        // Move to end, but before the closing bracket if it exists.
        // For simplicity in simulator we assume file is only appended and we'll trim later.
        if _, err := f.Seek(0, io.SeekEnd); err != nil {
            _ = f.Close()
            return nil, err
        }
    }

    return &MetricsCollector{file: f, firstDone: fi.Size() > 2}, nil // size > 2 means something after '['
}

// Record marshals metric and appends it as part of the JSON array.
func (mc *MetricsCollector) Record(m SchedulerMetrics) error {
    mc.mu.Lock()
    defer mc.mu.Unlock()

    m.Timestamp = time.Now()

    data, err := json.Marshal(m)
    if err != nil {
        return err
    }

    if mc.firstDone {
        if _, err := mc.file.WriteString(",\n"); err != nil {
            return err
        }
    }
    if _, err := mc.file.Write(data); err != nil {
        return err
    }
    mc.firstDone = true
    return nil
}

// Close finalises the JSON array and closes the underlying file.
func (mc *MetricsCollector) Close() error {
    mc.mu.Lock()
    defer mc.mu.Unlock()

    // write closing bracket and newline
    if _, err := mc.file.WriteString("\n]\n"); err != nil {
        return err
    }
    return mc.file.Close()
}
