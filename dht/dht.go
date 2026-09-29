package dht

import (
	"errors"
	"strings"
	"time"

	"github.com/ipfs/go-cid"
	record "github.com/libp2p/go-libp2p-record"
	"github.com/libp2p/go-libp2p/core/peer"
	mh "github.com/multiformats/go-multihash"

	krec "github.com/seanly/kether/record"
)

func AgentKey(agentID string) string {
	return "/kether/agent/1/" + agentID
}

func PeerKey(id peer.ID) string {
	return "/kether/bypeer/1/" + id.String()
}

func TypeCID(typ string) (cid.Cid, error) {
	sum, err := mh.Sum([]byte("kether/type/1/"+typ), mh.SHA2_256, -1)
	if err != nil {
		return cid.Undef, err
	}
	return cid.NewCidV1(cid.Raw, sum), nil
}

// Validator checks signed records stored under the "kether" namespace.
type Validator struct{}

func (Validator) Validate(key string, value []byte) error {
	if len(value) > krec.MaxBytes {
		return errors.New("dht: record too large")
	}
	rec, err := krec.Unmarshal(value)
	if err != nil {
		return err
	}
	if err := rec.Verify(time.Now()); err != nil {
		return err
	}
	_, rest, err := record.SplitKey(key)
	if err != nil {
		return err
	}
	if strings.HasPrefix(rest, "agent/1/") {
		id := strings.TrimPrefix(rest, "agent/1/")
		got, err := rec.AgentID()
		if err != nil {
			return err
		}
		if got != id {
			return errors.New("dht: agent id mismatch")
		}
	}
	return nil
}

func (Validator) Select(key string, values [][]byte) (int, error) {
	best := -1
	var bestSeq uint64
	for i, val := range values {
		rec, err := krec.Unmarshal(val)
		if err != nil {
			continue
		}
		if err := rec.Verify(time.Now()); err != nil {
			continue
		}
		if best < 0 || rec.Seq > bestSeq || (rec.Seq == bestSeq && i > best) {
			best = i
			bestSeq = rec.Seq
		}
	}
	if best < 0 {
		return 0, errors.New("dht: no valid record")
	}
	return best, nil
}
