module github.com/akitasoftware/akita-cli

go 1.15

require (
	github.com/AlecAivazis/survey/v2 v2.2.7
	github.com/OneOfOne/xxhash v1.2.8
	github.com/Pallinder/go-randomdata v1.2.0
	github.com/akitasoftware/akita-ir v0.0.0-20210818150446-55531f1ef499
	github.com/akitasoftware/akita-libs v0.0.0-20211014234051-fd212c925121
	github.com/akitasoftware/objecthash-proto v0.0.0-20210728061301-b7904b31cc09 // indirect
	github.com/andybalholm/brotli v1.0.1
	github.com/charmbracelet/glamour v0.2.0
	github.com/gdamore/tcell/v2 v2.1.0
	github.com/ghodss/yaml v1.0.0
	github.com/golang/gddo v0.0.0-20210115222349-20d68f94ee1f // indirect
	github.com/golang/mock v1.3.1
	// Golang protobuf APIv1, needed to compatibility with objecthash-proto. See
	// pb/README.md
	github.com/golang/protobuf v1.3.4
	github.com/google/go-cmp v0.5.4
	github.com/google/gopacket v1.1.19
	github.com/google/martian/v3 v3.0.1
	github.com/google/uuid v1.2.0
	github.com/gorilla/mux v1.8.0
	github.com/hashicorp/go-retryablehttp v0.6.8
	github.com/hashicorp/go-version v1.2.1
	github.com/jpillora/backoff v1.0.0
	github.com/logrusorgru/aurora v2.0.3+incompatible
	github.com/mitchellh/go-homedir v1.1.0
	github.com/patrickmn/go-cache v2.1.0+incompatible
	github.com/pkg/errors v0.9.1
	github.com/rivo/tview v0.0.0-20210217110421-8a8f78a6dd01
	github.com/spf13/cobra v1.1.3
	github.com/spf13/pflag v1.0.5
	github.com/spf13/viper v1.7.1
	github.com/stretchr/testify v1.7.0
	github.com/yudai/gojsondiff v1.0.0
	github.com/yudai/golcs v0.0.0-20170316035057-ecda9a501e82 // indirect
	golang.org/x/text v0.3.5
	gopkg.in/yaml.v2 v2.4.0
)

replace (
	// Merging google/gopacket into akitasoftware/gopacket does not
	// bring along any tags, such as the v1.1.19 release.
	github.com/google/gopacket v1.1.19 => github.com/akitasoftware/gopacket v1.1.18-0.20210730205736-879e93dac35b
	github.com/google/martian/v3 v3.0.1 => github.com/akitasoftware/martian/v3 v3.0.1-0.20210608174341-829c1134e9de
)
