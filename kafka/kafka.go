package akita

import (
	"context"
	"encoding/base64"
	"net"
	"os"
	"os/signal"
	"time"

	"github.com/confluentinc/confluent-kafka-go/kafka"
	"github.com/golang/protobuf/proto"
	"github.com/pkg/errors"

	"github.com/akitasoftware/akita-cli/learn"
	"github.com/akitasoftware/akita-cli/printer"
	"github.com/akitasoftware/akita-cli/rest"
	"github.com/akitasoftware/akita-cli/trace"
	"github.com/akitasoftware/akita-cli/util"
	pb "github.com/akitasoftware/akita-ir/go/api_spec"
	"github.com/akitasoftware/akita-libs/akid"
	kgxapi "github.com/akitasoftware/akita-libs/api_schema"
	"github.com/akitasoftware/akita-libs/pbhash"
)

type KafkaMessageUploader struct {
	client  rest.LearnClient
	session akid.LearnSessionID

	consumer *kafka.Consumer
	doneCh   chan struct{}
	errCh    chan error
}

func Run() error {
	// TODO: find topics by listing them
	topic := "t1"

	// TODO: replace by arguments
	c, err := kafka.NewConsumer(&kafka.ConfigMap{
		"bootstrap.servers": "localhost:49153",
		"group.id":          "akita-trace",
		"auto.offset.reset": "earliest",
	})
	if err != nil {
		return errors.Wrap(err, "failed to create consumer")
	}
	defer c.Close()

	err = c.SubscribeTopics([]string{topic}, nil)
	if err != nil {
		return errors.Wrap(err, "failed to subscribe")
	}

	// TODO: take as arguments
	domain := "localhost:50443"

	clientID := akid.GenerateClientID()
	service := "kafka-test"
	traceName := util.RandomLearnSessionName()

	frontClient := rest.NewFrontClient(domain, clientID)
	backendSvc, err := util.GetServiceIDByName(frontClient, service)
	if err != nil {
		return errors.Wrapf(err, "couldn't look up %q service", service)
	}
	learnClient := rest.NewLearnClient(domain, clientID, backendSvc)
	tags := make(map[string]string)
	backendLrn, err := util.NewLearnSession(domain, clientID, backendSvc, traceName, tags, nil)
	if err != nil {
		return errors.Wrapf(err, "couldn't create learn session %q", traceName)
	}
	printer.Infof("Created new trace on Akita Cloud: akita://%s:trace:%s ID %s\n", service, traceName, akid.String(backendLrn))

	uploader := &KafkaMessageUploader{
		client:  learnClient,
		session: backendLrn,

		consumer: c,
		doneCh:   make(chan struct{}),
		errCh:    make(chan error),
	}

	go uploader.readMessages()

	// Set up signal handler to stop packet processors on SIGINT or when one of
	// the processors returns an error.
	// Must use buffered channel for signals since the signal package does not
	// block when sending signals.
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)
	select {
	case <-sig:
		printer.Stderr.Infof("Received SIGINT, stopping trace collection...\n")
	case err = <-uploader.errCh:
		printer.Stderr.Errorf("Encountered error while collecting traces, stopping...\n")
		err = errors.Wrap(err, "error while collecting messagesr")
	}

	close(uploader.doneCh)

	return err

}

func (k *KafkaMessageUploader) parseMessage(msg *kafka.Message) {
	printer.Stderr.Infof("Message on %s: %s\n", msg.TopicPartition, string(msg.Value))

	data, err := learn.ParseBody("application/json", msg.Value, 200)
	if err != nil {
		printer.Stderr.Infof("Error parsing message value: %v\n", err)
		return
	}

	dataHash, err := pbhash.HashProto(data)
	if err != nil {
		printer.Stderr.Infof("Error hashing data: %v\n", err)
		return
	}

	// TODO: use a better identifier for the host
	// Hack: use the topic as a path template
	witness := &pb.Witness{
		Method: &pb.Method{
			Id: learn.UnassignedHTTPID(),
			Meta: &pb.MethodMeta{
				Meta: &pb.MethodMeta_Http{
					Http: &pb.HTTPMethodMeta{
						Method:       "GET",
						PathTemplate: "/" + *msg.TopicPartition.Topic,
						Host:         "kafka",
					},
				},
			},
			Responses: map[string]*pb.Data{
				dataHash: data,
			},
			Args: map[string]*pb.Data{},
		},
	}

	trace.Obfuscate(witness.Method)

	witnessHash, err := pbhash.HashProto(witness)
	if err != nil {
		printer.Stderr.Infof("Error hashing data: %v\n", err)
		return
	}

	b, err := proto.Marshal(witness)
	if err != nil {
		printer.Stderr.Infof("Error marshaling witness: %v\n", err)
		return
	}

	witnessReport := &kgxapi.WitnessReport{
		Direction:         kgxapi.Inbound,
		OriginAddr:        net.ParseIP("127.0.0.1"),
		OriginPort:        80,
		DestinationAddr:   net.ParseIP("!27.0.0.1"),
		DestinationPort:   80,
		ClientWitnessTime: msg.Timestamp,
		WitnessProto:      base64.URLEncoding.EncodeToString(b),
		Hash:              witnessHash,
		ID:                akid.GenerateWitnessID(),
	}

	printer.Stderr.Debugf("Witness: %s\n", witnessReport.WitnessProto)

	// TODO: batching
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err = k.client.ReportWitnesses(ctx, k.session, []*kgxapi.WitnessReport{witnessReport})
	if err != nil {
		printer.Stderr.Errorf("Error sending witness: %v\n", err)
		k.errCh <- err
	}
}

func (k *KafkaMessageUploader) readMessages() {
	c := k.consumer
	for {
		// Confluent deprecated a channel-based API in favor of time-based polling, I don't know why.
		msg, err := c.ReadMessage(1000)
		if err == nil {
			go func() {
				k.parseMessage(msg)
				c.CommitMessage(msg)
			}()
		} else {
			kErr := err.(kafka.Error)
			// Client automatically attempts to recover from errors, so just log?
			if kErr.Code() != kafka.ErrTimedOut {
				printer.Stderr.Errorf("Consumer error: %v (%v)\n", err, msg)
			}
		}

		select {
		case <-k.doneCh:
			return
		default:
		}
	}
}
