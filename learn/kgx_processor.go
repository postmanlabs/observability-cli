package learn

import (
	"context"
	"sync"
	"time"

	"github.com/akitasoftware/akita-cli/printer"
	"github.com/akitasoftware/akita-cli/rest"
	"github.com/akitasoftware/akita-libs/akid"
	kgxapi "github.com/akitasoftware/akita-libs/api_schema"
	"github.com/akitasoftware/akita-libs/batcher"
	"github.com/akitasoftware/akita-libs/tags"
)

const (
	witnessUploadTimeout = 10 * time.Second
)

type KGXReportError struct {
	message  string
	buffered []*kgxapi.WitnessReport
}

func (e KGXReportError) Error() string {
	return e.message
}

// implements witnessProcessor
type KGXWitnessProcessor struct {
	learnSessionID akid.LearnSessionID
	client         rest.LearnClient
	queue          *batcher.InMemory
	dir            kgxapi.NetworkDirection

	witnessTagsLock sync.RWMutex
	witnessTags     map[tags.Key]string
}

func NewKGXWitnessProcessor(lrn akid.LearnSessionID, client rest.LearnClient, bufferSize int, flushDuration time.Duration, dir kgxapi.NetworkDirection) *KGXWitnessProcessor {
	// Set defaults if zero values passed in constructor.
	if bufferSize == 0 {
		bufferSize = 1
	}
	if flushDuration == 0 {
		flushDuration = 1000 * time.Millisecond
	}

	kgxWriter := &KGXWitnessProcessor{
		learnSessionID: lrn,
		client:         client,
		dir:            dir,
	}
	q := batcher.NewInMemory(kgxWriter.uploadWitnesses, bufferSize, flushDuration)
	kgxWriter.queue = q
	return kgxWriter
}

func (w *KGXWitnessProcessor) ProcessWitness(r *witnessResult) error {
	report, err := r.toReport(w.dir)
	if err != nil {
		return err
	}

	w.witnessTagsLock.RLock()
	defer w.witnessTagsLock.RUnlock()
	report.Tags = w.witnessTags

	printer.V(4).Debugf("Adding witness\n")
	w.queue.Add(report)
	return nil
}

// Sets the tags for all witnesses going forward.
func (w *KGXWitnessProcessor) SetWitnessTags(tags map[tags.Key]string) {
	w.witnessTagsLock.Lock()
	defer w.witnessTagsLock.Unlock()
	w.witnessTags = tags
}

func (w *KGXWitnessProcessor) Close() {
	w.queue.Close()
}

// BatchProcessor function called by batcher in a single threaded context.
func (w *KGXWitnessProcessor) uploadWitnesses(batch []interface{}) {
	printer.V(4).Debugf("Uploading witnesses count=%d\n", len(batch))

	reports := make([]*kgxapi.WitnessReport, 0, len(batch))
	for _, b := range batch {
		reports = append(reports, b.(*kgxapi.WitnessReport))
	}

	ctx, cancel := context.WithTimeout(context.Background(), witnessUploadTimeout)
	defer cancel()
	req := &kgxapi.UploadReportsRequest{
		Witnesses: reports,
	}
	if err := w.client.AsyncReportsUpload(ctx, w.learnSessionID, req); err != nil {
		printer.Debugf("Failed to upload witnesses count=%d: %v\n", len(batch), err)
		printer.Warningf("Failed to upload some witnesses, Akita will generate partial results\n")
	}
}
