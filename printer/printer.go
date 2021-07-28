// printer package provides utility for displaying messages to users.
package printer

import (
	"fmt"
	"io"
	"os"

	"github.com/logrusorgru/aurora"
	"github.com/spf13/viper"
)

var (
	Stderr = NewP(os.Stderr)
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

type noopPrinter struct{}

func (noopPrinter) Infoln(args ...interface{})             {}
func (noopPrinter) Warningln(args ...interface{})          {}
func (noopPrinter) Errorln(args ...interface{})            {}
func (noopPrinter) Debugln(args ...interface{})            {}
func (noopPrinter) Infof(f string, args ...interface{})    {}
func (noopPrinter) Warningf(f string, args ...interface{}) {}
func (noopPrinter) Errorf(f string, args ...interface{})   {}
func (noopPrinter) Debugf(f string, args ...interface{})   {}
func (p noopPrinter) V(level int) P                        { return p }
