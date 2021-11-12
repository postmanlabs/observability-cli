package learn

import (
	"encoding/base64"
	"net"
	"time"

	"github.com/golang/protobuf/proto"
	"github.com/pkg/errors"

	"github.com/akitasoftware/akita-cli/printer"
	pb "github.com/akitasoftware/akita-ir/go/api_spec"
	"github.com/akitasoftware/akita-libs/akid"
	"github.com/akitasoftware/akita-libs/akinet"
	kgxapi "github.com/akitasoftware/akita-libs/api_schema"
	"github.com/akitasoftware/akita-libs/spec_util"
	"github.com/akitasoftware/akita-libs/spec_util/ir_hash"
)

const (
	// We stop trying to pair partial witnesses older than pairCacheExpiration.
	pairCacheExpiration = time.Minute

	// How often we clean out stale partial witnesses from pairCache.
	pairCacheCleanupInterval = 30 * time.Second
)

type witnessResult struct {
	srcIP           net.IP
	srcPort         uint16
	dstIP           net.IP
	dstPort         uint16
	witness         *pb.Witness
	observationTime time.Time
	id              akid.WitnessID
}

func (r witnessResult) toReport() (*kgxapi.WitnessReport, error) {
	// Hash algorithm defined in
	// https://docs.google.com/document/d/1ZANeoLTnsO10DcuzsAt6PBCt2MWLYW8oeu_A6d9bTJk/edit#heading=h.tbvm9waph6eu
	hash := ir_hash.HashWitnessToString(r.witness)

	b, err := proto.Marshal(r.witness)
	if err != nil {
		return nil, errors.Wrap(err, "failed to marshal witness proto")
	}

	return &kgxapi.WitnessReport{
		Direction:       kgxapi.Inbound,
		OriginAddr:      r.srcIP,
		OriginPort:      r.srcPort,
		DestinationAddr: r.dstIP,
		DestinationPort: r.dstPort,

		WitnessProto:      base64.URLEncoding.EncodeToString(b),
		ClientWitnessTime: r.observationTime,
		Hash:              hash,
		ID:                r.id,
	}, nil
}

// This can be applied to any field (cookies, query strings, etc).
type SensitiveDataMatcher func(string) bool

// Utility to consolidate matching and generate Akita Spec Annotation structure if a sensitive data match is found.
func CombineMatchers(matchers []SensitiveDataMatcher) SensitiveDataMatcher {
	return func(value string) bool {
		for _, m := range matchers {
			if m(value) {
				return true
			}
		}
		return false
	}
}

type PartialWitnessParser func(akinet.ParsedNetworkContent) (*PartialWitness, error)

// The returned channel is closed after elemChan closes.
func startLearning(elemChan <-chan akinet.ParsedNetworkTraffic) <-chan *witnessResult {
	resultChan := make(chan *witnessResult, 50)

	// Cache un-paired partial witnesses by pair key.
	pairCache := map[akid.WitnessID]*witnessResult{}

	go func() {
		defer close(resultChan)

		defer func() {
			// Flush any unpaired partial witnesses.
			for _, e := range pairCache {
				resultChan <- e
			}
		}()

		pairCacheCleanup := time.NewTicker(pairCacheCleanupInterval)
		defer pairCacheCleanup.Stop()

		for {
			select {
			case newElem, ok := <-elemChan:
				if !ok {
					return
				}

				parser, err := getPartialWitnessParser(newElem.Content)
				if err != nil {
					printer.V(4).Infof("Couldn't find witness parser for network traffic: %v\n", err)
					continue
				}

				partial, err := parser(newElem.Content)
				if err != nil {
					printer.Warningf("cannot convert network traffic to witness, skipping: %v\n", err)
					continue
				}

				if pair, ok := pairCache[partial.PairKey]; ok {
					// Combine the pair, merging the result into the existing item
					// rather than the new partial.
					MergeWitness(pair.witness, partial.Witness)
					delete(pairCache, partial.PairKey)

					// If newElem is the request, flip the src/dst in the pair before
					// reporting.
					if isRequest(newElem.Content) {
						pair.srcIP, pair.dstIP = pair.dstIP, pair.srcIP
						pair.srcPort, pair.dstPort = pair.dstPort, pair.srcPort
					}
					resultChan <- pair
				} else {
					// Store the partial witness for now, waiting for its pair or a
					// flush timeout.
					pairCache[partial.PairKey] = &witnessResult{
						srcIP:           newElem.SrcIP,
						srcPort:         uint16(newElem.SrcPort),
						dstIP:           newElem.DstIP,
						dstPort:         uint16(newElem.DstPort),
						witness:         partial.Witness,
						observationTime: newElem.ObservationTime,
						id:              partial.PairKey,
					}
				}
			case <-pairCacheCleanup.C:
				// Periodically clear up unpaired partial witnesses that are too old.
				cutoffTime := time.Now().Add(-pairCacheExpiration)
				for k, e := range pairCache {
					if e.observationTime.Before(cutoffTime) {
						resultChan <- e
						delete(pairCache, k)
					}
				}
			}
		}
	}()

	return resultChan
}

func getPartialWitnessParser(cont akinet.ParsedNetworkContent) (PartialWitnessParser, error) {
	switch t := cont.(type) {
	case akinet.HTTPRequest:
		return ParseHTTP, nil
	case akinet.HTTPResponse:
		return ParseHTTP, nil
	default:
		return nil, errors.Errorf("couldn't find witness parser for %T", t)
	}
}

func isRequest(cont akinet.ParsedNetworkContent) bool {
	switch cont.(type) {
	case akinet.HTTPRequest:
		return true
	default:
		return false
	}
}

func MergeWitness(dst, src *pb.Witness) {
	if dst.Method == nil {
		dst.Method = src.Method
		return
	}

	if dst.Method.Args == nil {
		dst.Method.Args = src.Method.Args
	} else {
		dst.Method.Responses = src.Method.Responses
	}

	if dst.Method.Meta == nil {
		dst.Method.Meta = src.Method.Meta
	}

	// Special HTTP handling - if dst is a witness of the response, populate HTTP
	// method meta from the src (the request witness).
	if httpMeta := spec_util.HTTPMetaFromMethod(dst.Method); httpMeta != nil && httpMeta.Method == "" {
		dst.Method.Meta = src.Method.Meta
	}
}
