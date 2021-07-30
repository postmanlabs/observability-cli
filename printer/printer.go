// printer package provides utility for displaying messages to users.
package printer

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/logrusorgru/aurora"
	"github.com/spf13/viper"
)

var (
	Stderr = NewP(os.Stderr)
	Stdout = NewP(os.Stdout)
	Color  = aurora.NewAurora(true)
)

func Infoln(args ...interface{}) {
	Stderr.Infoln(args...)
}

func Warningln(args ...interface{}) {
	Stderr.Warningln(args...)
}

func Errorln(args ...interface{}) {
	Stderr.Errorln(args...)
}

func Debugln(args ...interface{}) {
	Stderr.Debugln(args...)
}

func RawOutput(args ...interface{}) {
	Stderr.RawOutput(args...)
}

func Infof(fmtString string, args ...interface{}) {
	Stderr.Infof(fmtString, args...)
}

func Warningf(fmtString string, args ...interface{}) {
	Stderr.Warningf(fmtString, args...)
}

func Errorf(fmtString string, args ...interface{}) {
	Stderr.Errorf(fmtString, args...)
}

func Debugf(fmtString string, args ...interface{}) {
	Stderr.Debugf(fmtString, args...)
}

func V(level int) P {
	return Stderr.V(level)
}

type P interface {
	// Mimics the behavior of fmt.Println
	Infoln(args ...interface{})
	Warningln(args ...interface{})
	Errorln(args ...interface{})
	Debugln(args ...interface{})

	Infof(f string, args ...interface{})
	Warningf(f string, args ...interface{})
	Errorf(f string, args ...interface{})
	Debugf(f string, args ...interface{})
	V(level int) P

	// Output with no header
	RawOutput(args ...interface{})
}

type impl struct {
	out io.Writer
}

func NewP(out io.Writer) P {
	return impl{out: out}
}

func (p impl) ln(t string, args ...interface{}) {
	newArgs := make([]interface{}, 0, len(args)+1)
	newArgs = append(newArgs, t)
	for _, arg := range args {
		newArgs = append(newArgs, arg)
	}
	fmt.Fprintln(p.out, newArgs...)
}

func (p impl) Infoln(args ...interface{}) {
	p.ln(Color.Blue("[INFO] ").String(), args...)
}

func (p impl) Warningln(args ...interface{}) {
	p.ln(Color.Yellow("[WARNING] ").String(), args...)
}

func (p impl) Errorln(args ...interface{}) {
	p.ln(Color.Red("[ERROR] ").String(), args...)
}

func (p impl) Debugln(args ...interface{}) {
	if viper.GetBool("debug") {
		p.ln(Color.Magenta("[DEBUG] ").String(), args...)
	}
}

func (p impl) Infof(fmtString string, args ...interface{}) {
	fmt.Fprintf(p.out, Color.Blue("[INFO] ").String())
	fmt.Fprintf(p.out, fmtString, args...)
}

func (p impl) Warningf(fmtString string, args ...interface{}) {
	fmt.Fprintf(p.out, Color.Yellow("[WARNING] ").String())
	fmt.Fprintf(p.out, fmtString, args...)
}

func (p impl) Errorf(fmtString string, args ...interface{}) {
	fmt.Fprintf(p.out, Color.Red("[ERROR] ").String())
	fmt.Fprintf(p.out, fmtString, args...)
}

func (p impl) Debugf(fmtString string, args ...interface{}) {
	if viper.GetBool("debug") {
		fmt.Fprintf(p.out, Color.Magenta("[DEBUG] ").String())
		fmt.Fprintf(p.out, fmtString, args...)
	}
}

func (p impl) V(level int) P {
	if l := viper.GetInt("verbose-level"); l > 0 && level >= l {
		return p
	} else {
		return noopPrinter{}
	}
}

func (p impl) RawOutput(args ...interface{}) {
	fmt.Fprintln(p.out, args...)
}

type noopPrinter struct{}

func (noopPrinter) Infoln(args ...interface{})             {}
func (noopPrinter) Warningln(args ...interface{})          {}
func (noopPrinter) Errorln(args ...interface{})            {}
func (noopPrinter) Debugln(args ...interface{})            {}
func (noopPrinter) RawOutput(args ...interface{})          {}
func (noopPrinter) Infof(f string, args ...interface{})    {}
func (noopPrinter) Warningf(f string, args ...interface{}) {}
func (noopPrinter) Errorf(f string, args ...interface{})   {}
func (noopPrinter) Debugf(f string, args ...interface{})   {}
func (p noopPrinter) V(level int) P                        { return p }

type jsonImpl struct {
	encoder *json.Encoder
}

func SwitchToJSON() {
	// No ANSI escapes
	Color = aurora.NewAurora(false)
	Stderr = &jsonImpl{
		encoder: json.NewEncoder(os.Stderr),
	}
	Stdout = &jsonImpl{
		encoder: json.NewEncoder(os.Stdout),
	}
}

func SwitchToPlain() {
	// No ANSI escapes
	Color = aurora.NewAurora(false)
}

// A JSON log entry using the reserved fields that DataDog expects
// to find.  We assume that "host", "service", and "env" will
// be filled in by the collector.
type jsonLog struct {
	Date    time.Time `json:"date"`
	Status  string    `json:"status"`
	Message string    `json:"message"`
}

func (j *jsonImpl) writeJSON(status string, message string) {
	message = strings.Trim(message, "\n")
	logEntry := jsonLog{
		Date:    time.Now(),
		Status:  status,
		Message: message,
	}
	j.encoder.Encode(logEntry) // includes newline!
}

func (j *jsonImpl) Infoln(args ...interface{}) {
	j.writeJSON("info", fmt.Sprint(args...))
}

func (j *jsonImpl) Warningln(args ...interface{}) {
	j.writeJSON("warning", fmt.Sprint(args...))
}

func (j *jsonImpl) Errorln(args ...interface{}) {
	j.writeJSON("error", fmt.Sprint(args...))
}

func (j *jsonImpl) Debugln(args ...interface{}) {
	if viper.GetBool("debug") {
		j.writeJSON("debug", fmt.Sprint(args...))
	}
}

func (j *jsonImpl) RawOutput(args ...interface{}) {
	// omit
}

func (j *jsonImpl) Infof(f string, args ...interface{}) {
	j.writeJSON("info", fmt.Sprintf(f, args...))
}

func (j *jsonImpl) Warningf(f string, args ...interface{}) {
	j.writeJSON("warning", fmt.Sprintf(f, args...))
}

func (j *jsonImpl) Errorf(f string, args ...interface{}) {
	j.writeJSON("error", fmt.Sprintf(f, args...))
}

func (j *jsonImpl) Debugf(f string, args ...interface{}) {
	if viper.GetBool("debug") {
		j.writeJSON("debug", fmt.Sprintf(f, args...))
	}
}

func (j *jsonImpl) V(level int) P {
	if l := viper.GetInt("verbose-level"); l > 0 && level >= l {
		return j
	} else {
		return noopPrinter{}
	}
}
