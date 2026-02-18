package api

import (
"encoding/json"
"fmt"
"net/http"
"sort"
"strconv"
"strings"
"time"
)

// Loki-compatible API for Grafana integration
// Grafana connects to Central as a Loki datasource and queries logs from here

// registerLokiRoutes registers Loki-compatible endpoints
func (h *Handler) registerLokiRoutes(mux *http.ServeMux) {
mux.HandleFunc("/loki/api/v1/query_range", h.handleLokiQueryRange)
mux.HandleFunc("/loki/api/v1/labels", h.handleLokiLabels)
mux.HandleFunc("/loki/api/v1/label/", h.handleLokiLabelValues)
mux.HandleFunc("/loki/api/v1/ready", h.handleLokiReady)
}

func (h *Handler) handleLokiReady(w http.ResponseWriter, r *http.Request) {
w.Header().Set("Content-Type", "text/plain")
w.Write([]byte("ready"))
}

func (h *Handler) handleLokiLabels(w http.ResponseWriter, r *http.Request) {
w.Header().Set("Content-Type", "application/json")
json.NewEncoder(w).Encode(map[string]interface{}{
"status": "success",
"data":   []string{"agent", "agentId", "type", "status", "vm"},
})
}

func (h *Handler) handleLokiLabelValues(w http.ResponseWriter, r *http.Request) {
w.Header().Set("Content-Type", "application/json")
label := strings.TrimPrefix(r.URL.Path, "/loki/api/v1/label/")
label = strings.TrimSuffix(label, "/values")

values, err := h.store.GetLogLabels(label)
if err != nil {
values = []string{}
}
sort.Strings(values)
json.NewEncoder(w).Encode(map[string]interface{}{
"status": "success",
"data":   values,
})
}

// handleLokiQueryRange implements /loki/api/v1/query_range
// Returns logs in Loki streams format that Grafana understands
func (h *Handler) handleLokiQueryRange(w http.ResponseWriter, r *http.Request) {
w.Header().Set("Content-Type", "application/json")

query := r.URL.Query().Get("query")
startStr := r.URL.Query().Get("start")
endStr := r.URL.Query().Get("end")
limitStr := r.URL.Query().Get("limit")

limit := 1000
if v, err := strconv.Atoi(limitStr); err == nil && v > 0 {
limit = v
}
if limit > 5000 {
limit = 5000
}

var from, to time.Time
if startStr != "" {
from = parseNanoOrRFC(startStr)
}
if endStr != "" {
to = parseNanoOrRFC(endStr)
}
if from.IsZero() {
from = time.Now().Add(-1 * time.Hour)
}
if to.IsZero() {
to = time.Now()
}

// Parse LogQL-like selector: {agent="xxx", type="Backup"}
agentFilter, typeFilter, statusFilter, vmFilter := parseLokiSelector(query)

logs, err := h.store.QueryLogs(agentFilter, typeFilter, statusFilter, from, to, limit)
if err != nil {
json.NewEncoder(w).Encode(map[string]interface{}{
"status": "error",
"error":  err.Error(),
})
return
}

// Apply VM filter
if vmFilter != "" {
filtered := logs[:0]
for _, l := range logs {
if strings.EqualFold(l.TargetVM, vmFilter) {
filtered = append(filtered, l)
}
}
logs = filtered
}

// Group logs by stream labels (agent + type)
type streamKey struct {
agentID   string
agentName string
logType   string
}
streams := make(map[streamKey][][2]string)
for _, log := range logs {
sk := streamKey{agentID: log.AgentID, agentName: log.AgentName, logType: log.Type}
ts := fmt.Sprintf("%d", log.Timestamp.UnixNano())
line := log.Message
if log.TargetVM != "" {
line = fmt.Sprintf("[%s] [%s] %s", log.TargetVM, log.Status, log.Message)
} else {
line = fmt.Sprintf("[%s] %s", log.Status, log.Message)
}
streams[sk] = append(streams[sk], [2]string{ts, line})
}

// Build Loki response
var resultStreams []map[string]interface{}
for sk, values := range streams {
resultStreams = append(resultStreams, map[string]interface{}{
"stream": map[string]string{
"agent":   sk.agentName,
"agentId": sk.agentID,
"type":    sk.logType,
},
"values": values,
})
}
if resultStreams == nil {
resultStreams = []map[string]interface{}{}
}

json.NewEncoder(w).Encode(map[string]interface{}{
"status": "success",
"data": map[string]interface{}{
"resultType": "streams",
"result":     resultStreams,
},
})
}

// parseLokiSelector parses a simple LogQL selector like {agent="name", type="Backup"}
func parseLokiSelector(query string) (agentID, logType, status, vm string) {
query = strings.TrimSpace(query)
query = strings.Trim(query, "{}")
if query == "" {
return
}
parts := strings.Split(query, ",")
for _, p := range parts {
p = strings.TrimSpace(p)
kv := strings.SplitN(p, "=", 2)
if len(kv) != 2 {
continue
}
key := strings.TrimSpace(kv[0])
val := strings.Trim(strings.TrimSpace(kv[1]), `"'~`)
switch key {
case "agentId":
agentID = val
case "type":
logType = val
case "status":
status = val
case "vm":
vm = val
}
}
return
}

// parseNanoOrRFC parses Loki-style nanosecond timestamp or RFC3339
func parseNanoOrRFC(s string) time.Time {
s = strings.TrimSpace(s)
if s == "" {
return time.Time{}
}
// Try nanosecond unix timestamp
if n, err := strconv.ParseInt(s, 10, 64); err == nil {
if n > 1e18 { // nanoseconds
return time.Unix(0, n)
}
if n > 1e15 { // microseconds
return time.Unix(0, n*1000)
}
if n > 1e12 { // milliseconds
return time.Unix(0, n*1000000)
}
return time.Unix(n, 0) // seconds
}
// Try RFC3339
if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
return t
}
if t, err := time.Parse(time.RFC3339, s); err == nil {
return t
}
return time.Time{}
}
