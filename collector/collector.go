package collector

import "github.com/prometheus/client_golang/prometheus"

type OpenGNBCollector struct {
	collectors         []prometheus.Collector
	receiveBytes       *prometheus.GaugeVec
	transmitBytes      *prometheus.GaugeVec
	keepAliveTimestamp *prometheus.GaugeVec
	nodeState          *prometheus.GaugeVec
	addr4PingLatency   *prometheus.GaugeVec
	addr6PingLatency   *prometheus.GaugeVec
}

func NewOpenGNBCollector() *OpenGNBCollector {
	c := &OpenGNBCollector{}

	c.receiveBytes = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "opengnb",
		Subsystem: "network",
		Name:      "receive_bytes_total",
		Help:      "OpenGNB network statistic receive_bytes",
	}, []string{"network", "node"})
	c.collectors = append(c.collectors, c.receiveBytes)

	c.transmitBytes = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "opengnb",
		Subsystem: "network",
		Name:      "transmit_bytes_total",
		Help:      "OpenGNB network statistic transmit_bytes",
	}, []string{"network", "node"})
	c.collectors = append(c.collectors, c.transmitBytes)

	c.keepAliveTimestamp = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "opengnb",
		Subsystem: "status",
		Name:      "keep_alive_timestamp",
		Help:      "OpenGNB keep alive timestamp",
	}, []string{"network"})
	c.collectors = append(c.collectors, c.keepAliveTimestamp)

	c.nodeState = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "opengnb",
		Subsystem: "node",
		Name:      "state",
		Help:      "OpenGNB node state",
	}, []string{"network", "node"})
	c.collectors = append(c.collectors, c.nodeState)

	c.addr4PingLatency = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "opengnb",
		Subsystem: "node",
		Name:      "addr4_ping_latency_ms",
		Help:      "OpenGNB network IPv4 address ping latency",
	}, []string{"network", "node"})
	c.collectors = append(c.collectors, c.addr4PingLatency)

	c.addr6PingLatency = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "opengnb",
		Subsystem: "node",
		Name:      "addr6_ping_latency_ms",
		Help:      "OpenGNB network IPv6 address ping latency",
	}, []string{"network", "node"})
	c.collectors = append(c.collectors, c.addr6PingLatency)

	return c
}

func (c *OpenGNBCollector) SetReceiveBytes(network, node string, value uint64) {
	c.receiveBytes.WithLabelValues(network, node).Set(float64(value))
}

func (c *OpenGNBCollector) SetTransmitBytes(network, node string, value uint64) {
	c.transmitBytes.WithLabelValues(network, node).Set(float64(value))
}

func (c *OpenGNBCollector) SetKeepAliveTimestamp(network string, value int64) {
	c.keepAliveTimestamp.WithLabelValues(network).Set(float64(value))
}

func (c *OpenGNBCollector) SetNodeState(network, node string, value uint8) {
	c.nodeState.WithLabelValues(network, node).Set(float64(value))
}

func (c *OpenGNBCollector) SetAddr4PingLatency(network, node string, value int64) {
	c.addr4PingLatency.WithLabelValues(network, node).Set(float64(value))
}

func (c *OpenGNBCollector) SetAddr6PingLatency(network, node string, value int64) {
	c.addr6PingLatency.WithLabelValues(network, node).Set(float64(value))
}

func (c *OpenGNBCollector) Reset() {
	for _, collector := range c.collectors {
		if c, ok := collector.(interface {
			Reset()
		}); ok {
			c.Reset()
		}
	}
}
func (c *OpenGNBCollector) Collect(ch chan<- prometheus.Metric) {
	for _, collector := range c.collectors {
		collector.Collect(ch)
	}
}
func (c *OpenGNBCollector) Describe(ch chan<- *prometheus.Desc) {
	for _, collector := range c.collectors {
		collector.Describe(ch)
	}
}
