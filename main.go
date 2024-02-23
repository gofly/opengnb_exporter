package main

import (
	"bytes"
	"encoding/binary"
	"errors"
	"flag"
	"io"
	"strings"
	"time"

	"log"
	"net/http"
	"strconv"

	"github.com/gofly/opengnb_exporter/collector"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"golang.org/x/exp/mmap"
)

const (
	GNB_CTL_MAGIC_NUMBER = 2
	GNB_CTL_CONF         = 3
	GNB_CTL_CORE         = 4
	GNB_CTL_STATUS       = 5
	GNB_CTL_NODE         = 6
)

// copy from the result of showsizeoffset in https://github.com/gofly/opengnb/tree/main/src/cli
const (
	sizeof__uint32_t      = 4
	sizeof__gnb_block32_t = 4

	sizeof__gnb_ctl_core_zone_t             = 159584
	sizeof__gnb_ctl_core_zone_t__local_uuid = 4
	sizeof__gnb_ctl_core_zone_t__ifname     = 256

	sizeof__gnb_ctl_status_zone_t                    = 8
	sizeof__gnb_ctl_status_zone_t__keep_alive_ts_sec = 8

	sizeof__gnb_ctl_node_zone_t           = 16
	sizeof__gnb_ctl_node_zone_t__name     = 8
	sizeof__gnb_ctl_node_zone_t__node_num = 4

	sizeof__gnb_node_t                          = 3192
	sizeof__gnb_node_t__uuid32                  = 4
	sizeof__gnb_node_t__in_bytes                = 8
	sizeof__gnb_node_t__out_bytes               = 8
	sizeof__gnb_node_t__udp_addr_status         = 4
	sizeof__gnb_node_t__addr6_ping_latency_usec = 8
	sizeof__gnb_node_t__addr4_ping_latency_usec = 8
)

const (
	offsetof__gnb_ctl_core_zone_t__local_uuid = 8
	offsetof__gnb_ctl_core_zone_t__ifname     = 112

	offsetof__gnb_ctl_status_zone_t__keep_alive_ts_sec = 0

	offsetof__gnb_ctl_node_zone_t__name     = 0
	offsetof__gnb_ctl_node_zone_t__node_num = 8

	offsetof__gnb_node_t__uuid32                  = 0
	offsetof__gnb_node_t__in_bytes                = 8
	offsetof__gnb_node_t__out_bytes               = 16
	offsetof__gnb_node_t__udp_addr_status         = 2096
	offsetof__gnb_node_t__addr6_ping_latency_usec = 2104
	offsetof__gnb_node_t__addr4_ping_latency_usec = 2112
)

type NodeState uint8

const (
	NodeStateUnreachable NodeState = 0
	NodeStateIPv4Direct  NodeState = 0x1 << (iota - 1)
	NodeStateIPv6Direct
)

var ErrMagicNotMatch = errors.New("magic not match")

type ZoneOffset struct {
	MagicZone  int64
	ConfZone   int64
	CoreZone   int64
	StatusZone int64
	NodeZone   int64
}

type Block struct {
	Size int
	Data []byte
}

type NodeID uint32

func (v NodeID) String() string {
	return strconv.FormatUint(uint64(v), 10)
}

type Node struct {
	NodeID           NodeID
	InBytes          uint64
	OutBytes         uint64
	NodeState        NodeState
	Addr4PingLatency int64
	Addr6PingLatency int64
}

type String []byte

func (s String) String() string {
	idx := bytes.IndexByte(s, 0)
	if idx == -1 {
		return ""
	}
	return string(s[:idx])
}

type NodeZone struct {
	Name    String
	NodeNum int
	Nodes   []*Node
}

type StatusZone struct {
	KeepAliveTimestamp int64
}

type CoreZone struct {
	LocalUUID NodeID
	IFName    String
}

type GnbController struct {
	mmapPath string
}

func NewGnbController(mmapPath string) *GnbController {
	return &GnbController{
		mmapPath: mmapPath,
	}
}

func (c *GnbController) Invoke(fn func(io.ReaderAt) error) error {
	ra, err := mmap.Open(c.mmapPath)
	if err != nil {
		return err
	}
	defer ra.Close()

	err = c.validateMagic(ra)
	if err != nil {
		return err
	}
	return fn(ra)
}

func (c *GnbController) validateMagic(ra io.ReaderAt) error {
	magic := make([]byte, 3)
	_, err := ra.ReadAt(magic, 0)
	if err != nil {
		return err
	}
	if !bytes.Equal(magic, []byte{'G', 'N', 'B'}) {
		return ErrMagicNotMatch
	}
	return nil
}

func (c *GnbController) ReadZoneOffset(ra io.ReaderAt) (*ZoneOffset, error) {
	offsets := make([]byte, sizeof__uint32_t*8)
	_, err := ra.ReadAt(offsets, 0)
	if err != nil {
		return nil, err
	}
	return &ZoneOffset{
		MagicZone:  int64(binary.LittleEndian.Uint32(offsets[sizeof__uint32_t*GNB_CTL_MAGIC_NUMBER : sizeof__uint32_t*(GNB_CTL_MAGIC_NUMBER+1)])),
		ConfZone:   int64(binary.LittleEndian.Uint32(offsets[sizeof__uint32_t*GNB_CTL_CONF : sizeof__uint32_t*(GNB_CTL_CONF+1)])),
		CoreZone:   int64(binary.LittleEndian.Uint32(offsets[sizeof__uint32_t*GNB_CTL_CORE : sizeof__uint32_t*(GNB_CTL_CORE+1)])),
		StatusZone: int64(binary.LittleEndian.Uint32(offsets[sizeof__uint32_t*GNB_CTL_STATUS : sizeof__uint32_t*(GNB_CTL_STATUS+1)])),
		NodeZone:   int64(binary.LittleEndian.Uint32(offsets[sizeof__uint32_t*GNB_CTL_NODE : sizeof__uint32_t*(GNB_CTL_NODE+1)])),
	}, nil
}

func (c *GnbController) readBlock(ra io.ReaderAt, offset int64) (*Block, error) {
	blockSize, offset, err := c.readBlockSize(ra, offset)
	if err != nil {
		return nil, err
	}
	block := &Block{
		Size: blockSize,
		Data: make([]byte, blockSize),
	}
	_, err = ra.ReadAt(block.Data, offset)
	return block, err
}

func (c *GnbController) readBlockSize(ra io.ReaderAt, offset int64) (int, int64, error) {
	blockSizeData := make([]byte, sizeof__gnb_block32_t)
	_, err := ra.ReadAt(blockSizeData, offset)
	if err != nil {
		return 0, 0, err
	}
	offset += 4
	blockSize := int(binary.LittleEndian.Uint32(blockSizeData))
	return blockSize, offset, nil
}

func (c *GnbController) ReadStatusZone(ra io.ReaderAt, offset int64) (*StatusZone, error) {
	offset += sizeof__gnb_block32_t // skip sizeof(gnb_block32_t)
	statusData := make([]byte, sizeof__gnb_ctl_status_zone_t)
	_, err := ra.ReadAt(statusData, offset)
	if err != nil {
		return nil, err
	}
	return &StatusZone{
		KeepAliveTimestamp: int64(binary.LittleEndian.Uint64(statusData[offsetof__gnb_ctl_status_zone_t__keep_alive_ts_sec : offsetof__gnb_ctl_status_zone_t__keep_alive_ts_sec+sizeof__gnb_ctl_status_zone_t__keep_alive_ts_sec])),
	}, nil
}

func (c *GnbController) ReadCoreZone(ra io.ReaderAt, offset int64) (*CoreZone, error) {
	offset += sizeof__gnb_block32_t // skip sizeof(gnb_block32_t)
	coreData := make([]byte, sizeof__gnb_ctl_core_zone_t)
	_, err := ra.ReadAt(coreData, offset)
	if err != nil {
		return nil, err
	}
	return &CoreZone{
		LocalUUID: NodeID(binary.LittleEndian.Uint32(coreData[offsetof__gnb_ctl_core_zone_t__local_uuid : offsetof__gnb_ctl_core_zone_t__local_uuid+sizeof__gnb_ctl_core_zone_t__local_uuid])),
		IFName:    String(coreData[offsetof__gnb_ctl_core_zone_t__ifname : offsetof__gnb_ctl_core_zone_t__ifname+sizeof__gnb_ctl_core_zone_t__ifname]),
	}, nil
}

func (c *GnbController) ReadNodeZone(ra io.ReaderAt, offset int64) (*NodeZone, error) {
	offset += sizeof__gnb_block32_t // skip sizeof(gnb_block32_t)
	nodeZoneData := make([]byte, sizeof__gnb_ctl_node_zone_t)
	_, err := ra.ReadAt(nodeZoneData, offset)
	if err != nil {
		return nil, err
	}
	offset += sizeof__gnb_ctl_node_zone_t // skip sizeof(gnb_ctl_node_zone_t)
	nodeNum := int(binary.LittleEndian.Uint32(nodeZoneData[offsetof__gnb_ctl_node_zone_t__node_num : offsetof__gnb_ctl_node_zone_t__node_num+sizeof__gnb_ctl_node_zone_t__node_num]))
	nodeZone := &NodeZone{
		Name:    String(nodeZoneData[offsetof__gnb_ctl_node_zone_t__name:sizeof__gnb_ctl_node_zone_t__name]),
		NodeNum: nodeNum,
		Nodes:   make([]*Node, 0, nodeNum),
	}

	for i := 0; i < nodeZone.NodeNum; i++ {
		// sizeof(gnb_node_t)
		nodeData := make([]byte, sizeof__gnb_node_t)
		_, err := ra.ReadAt(nodeData, offset)
		if err != nil {
			return nil, err
		}
		offset += int64(len(nodeData))

		node := &Node{
			NodeID:           NodeID(binary.LittleEndian.Uint32(nodeData[offsetof__gnb_node_t__uuid32 : offsetof__gnb_node_t__uuid32+sizeof__gnb_node_t__uuid32])),
			InBytes:          binary.LittleEndian.Uint64(nodeData[offsetof__gnb_node_t__in_bytes : offsetof__gnb_node_t__in_bytes+sizeof__gnb_node_t__in_bytes]),
			OutBytes:         binary.LittleEndian.Uint64(nodeData[offsetof__gnb_node_t__out_bytes : offsetof__gnb_node_t__out_bytes+sizeof__gnb_node_t__out_bytes]),
			Addr4PingLatency: int64(binary.LittleEndian.Uint64(nodeData[offsetof__gnb_node_t__addr4_ping_latency_usec : offsetof__gnb_node_t__addr4_ping_latency_usec+sizeof__gnb_node_t__addr4_ping_latency_usec])),
			Addr6PingLatency: int64(binary.LittleEndian.Uint64(nodeData[offsetof__gnb_node_t__addr6_ping_latency_usec : offsetof__gnb_node_t__addr6_ping_latency_usec+sizeof__gnb_node_t__addr6_ping_latency_usec])),
		}
		udpAddrStatus := binary.LittleEndian.Uint32(nodeData[offsetof__gnb_node_t__udp_addr_status : offsetof__gnb_node_t__udp_addr_status+sizeof__gnb_node_t__udp_addr_status])
		if udpAddrStatus&(0x1<<2) > 0 {
			node.NodeState |= NodeStateIPv4Direct
		}
		if udpAddrStatus&(0x1<<3) > 0 {
			node.NodeState |= NodeStateIPv6Direct
		}
		nodeZone.Nodes = append(nodeZone.Nodes, node)
	}
	return nodeZone, nil
}

func main() {
	addr := flag.String("addr", ":9102", "port to serve")
	mmap := flag.String("mmap", "/var/run/gnb.map", "opengnb mmap file path")
	flag.Parse()

	ctrl := NewGnbController(*mmap)

	c := collector.NewOpenGNBCollector()
	prometheus.MustRegister(c)

	handler := promhttp.Handler()
	http.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		c.Reset()
		err := ctrl.Invoke(func(ra io.ReaderAt) error {
			offset, err := ctrl.ReadZoneOffset(ra)
			if err != nil {
				return err
			}

			cz, err := ctrl.ReadCoreZone(ra, offset.CoreZone)
			if err != nil {
				return err
			}

			sz, err := ctrl.ReadStatusZone(ra, offset.StatusZone)
			if err != nil {
				return err
			}

			network := strings.TrimPrefix(cz.IFName.String(), "gnb-")

			c.SetKeepAliveTimestamp(network, sz.KeepAliveTimestamp)

			if time.Now().Unix()-sz.KeepAliveTimestamp > 30 {
				return nil
			}

			nz, err := ctrl.ReadNodeZone(ra, offset.NodeZone)
			if err != nil {
				return err
			}

			for _, node := range nz.Nodes {
				if node.NodeID == cz.LocalUUID {
					continue
				}
				nodeID := node.NodeID.String()
				c.SetReceiveBytes(network, nodeID, node.OutBytes)
				c.SetTransmitBytes(network, nodeID, node.InBytes)
				c.SetNodeState(network, nodeID, uint8(node.NodeState))
				c.SetAddr4PingLatency(network, nodeID, node.Addr4PingLatency/1000)
				c.SetAddr6PingLatency(network, nodeID, node.Addr6PingLatency/1000)
			}
			return nil
		})
		if err != nil {
			log.Println(err)
		}
		handler.ServeHTTP(w, r)
	})
	log.Fatal(http.ListenAndServe(*addr, nil))
}
