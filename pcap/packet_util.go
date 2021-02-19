package pcap

import (
	"net"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
)

func CreatePacket(src, dst net.IP, srcPort, dstPort int, payload []byte) gopacket.Packet {
	return CreatePacketWithSeq(src, dst, srcPort, dstPort, payload, 0)
}

func createPacketLayers(src, dst net.IP, srcPort, dstPort int, seq uint32) (*layers.Ethernet, *layers.IPv4, *layers.TCP) {
	ethernetLayer := &layers.Ethernet{
		EthernetType: layers.EthernetTypeIPv4,
		SrcMAC:       net.HardwareAddr{0xFF, 0xAA, 0xFA, 0xAA, 0xFF, 0xAA},
		DstMAC:       net.HardwareAddr{0xBD, 0xBD, 0xBD, 0xBD, 0xBD, 0xBD},
	}
	ipLayer := &layers.IPv4{
		Protocol: layers.IPProtocolTCP,
		SrcIP:    src,
		DstIP:    dst,
	}
	tcpLayer := &layers.TCP{
		SrcPort: layers.TCPPort(srcPort),
		DstPort: layers.TCPPort(dstPort),
		Seq:     seq,
	}
	return ethernetLayer, ipLayer, tcpLayer
}

func CreateTCPSYN(src, dst net.IP, srcPort, dstPort int, seq uint32) gopacket.Packet {
	ethernetLayer, ipLayer, tcpLayer := createPacketLayers(src, dst, srcPort, dstPort, seq)
	tcpLayer.SYN = true
	buffer := gopacket.NewSerializeBuffer()
	opts := gopacket.SerializeOptions{FixLengths: true}
	gopacket.SerializeLayers(buffer, opts, ethernetLayer, ipLayer, tcpLayer)
	return gopacket.NewPacket(buffer.Bytes(), layers.LayerTypeEthernet, gopacket.Default)
}

func CreateTCPSYNAndACK(src, dst net.IP, srcPort, dstPort int, seq uint32) gopacket.Packet {
	ethernetLayer, ipLayer, tcpLayer := createPacketLayers(src, dst, srcPort, dstPort, seq)
	tcpLayer.SYN = true
	tcpLayer.ACK = true
	buffer := gopacket.NewSerializeBuffer()
	opts := gopacket.SerializeOptions{FixLengths: true}
	gopacket.SerializeLayers(buffer, opts, ethernetLayer, ipLayer, tcpLayer)
	return gopacket.NewPacket(buffer.Bytes(), layers.LayerTypeEthernet, gopacket.Default)
}

func CreatePacketWithSeq(src, dst net.IP, srcPort, dstPort int, payload []byte, seq uint32) gopacket.Packet {
	ethernetLayer, ipLayer, tcpLayer := createPacketLayers(src, dst, srcPort, dstPort, seq)
	buffer := gopacket.NewSerializeBuffer()
	opts := gopacket.SerializeOptions{FixLengths: true}
	gopacket.SerializeLayers(buffer, opts,
		ethernetLayer,
		ipLayer,
		tcpLayer,
		gopacket.Payload(payload),
	)
	return gopacket.NewPacket(buffer.Bytes(), layers.LayerTypeEthernet, gopacket.Default)
}

func CreateUDPPacket(src, dst net.IP, srcPort, dstPort int, payload []byte) gopacket.Packet {
	ethernetLayer := &layers.Ethernet{
		EthernetType: layers.EthernetTypeIPv4,
		SrcMAC:       net.HardwareAddr{0xFF, 0xAA, 0xFA, 0xAA, 0xFF, 0xAA},
		DstMAC:       net.HardwareAddr{0xBD, 0xBD, 0xBD, 0xBD, 0xBD, 0xBD},
	}
	ipLayer := &layers.IPv4{
		Protocol: layers.IPProtocolUDP,
		SrcIP:    src,
		DstIP:    dst,
	}
	udpLayer := &layers.UDP{
		SrcPort: layers.UDPPort(srcPort),
		DstPort: layers.UDPPort(dstPort),
	}

	buffer := gopacket.NewSerializeBuffer()
	opts := gopacket.SerializeOptions{FixLengths: true}
	gopacket.SerializeLayers(buffer, opts,
		ethernetLayer,
		ipLayer,
		udpLayer,
		gopacket.Payload(payload),
	)
	return gopacket.NewPacket(buffer.Bytes(), layers.LayerTypeEthernet, gopacket.Default)
}
