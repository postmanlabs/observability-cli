module github.com/akitasoftware/akita-cli

go 1.18

require (
	github.com/AlecAivazis/survey/v2 v2.3.2
	github.com/OneOfOne/xxhash v1.2.8
	github.com/Pallinder/go-randomdata v1.2.0
	github.com/akitasoftware/akita-ir v0.0.0-20220630210013-8926783978fe
	github.com/akitasoftware/akita-libs v0.0.0-20220920043137-ab41e1285024
	github.com/akitasoftware/go-utils v0.0.0-20220606224752-aad0f81bb9e7
	github.com/akitasoftware/plugin-flickr v0.2.0
	github.com/andybalholm/brotli v1.0.1
	github.com/gdamore/tcell/v2 v2.1.0
	github.com/ghodss/yaml v1.0.0
	github.com/golang/gddo v0.0.0-20210115222349-20d68f94ee1f
	github.com/golang/mock v1.3.1
	github.com/golang/protobuf v1.5.2
	github.com/google/go-cmp v0.5.6
	github.com/google/gopacket v1.1.19
	github.com/google/martian/v3 v3.0.1
	github.com/google/uuid v1.3.0
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
	github.com/stretchr/testify v1.7.1
	github.com/yudai/gojsondiff v1.0.0
	golang.org/x/exp v0.0.0-20220428152302-39d4317da171
	golang.org/x/text v0.3.7
	gopkg.in/yaml.v2 v2.4.0
)

require (
	github.com/akitasoftware/objecthash-proto v0.0.0-20211020162104-173a34b1afb0 // indirect
	github.com/bmizerany/assert v0.0.0-20160611221934-b7ed37b82869 // indirect
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/dukex/mixpanel v1.0.1 // indirect
	github.com/fsnotify/fsnotify v1.4.9 // indirect
	github.com/gdamore/encoding v1.0.0 // indirect
	github.com/golang/glog v0.0.0-20160126235308-23def4e6c14b // indirect
	github.com/hashicorp/errwrap v1.0.0 // indirect
	github.com/hashicorp/go-cleanhttp v0.5.1 // indirect
	github.com/hashicorp/go-multierror v1.1.1 // indirect
	github.com/hashicorp/hcl v1.0.0 // indirect
	github.com/inconshreveable/mousetrap v1.0.0 // indirect
	github.com/kballard/go-shellquote v0.0.0-20180428030007-95032a82bc51 // indirect
	github.com/lucasb-eyer/go-colorful v1.0.3 // indirect
	github.com/magiconair/properties v1.8.1 // indirect
	github.com/mattn/go-colorable v0.1.6 // indirect
	github.com/mattn/go-isatty v0.0.12 // indirect
	github.com/mattn/go-runewidth v0.0.10 // indirect
	github.com/mgutz/ansi v0.0.0-20170206155736-9520e82c474b // indirect
	github.com/mitchellh/mapstructure v1.1.2 // indirect
	github.com/pelletier/go-toml v1.2.0 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	github.com/rivo/uniseg v0.2.0 // indirect
	github.com/segmentio/analytics-go/v3 v3.2.1 // indirect
	github.com/segmentio/backo-go v1.0.0 // indirect
	github.com/sergi/go-diff v1.1.0 // indirect
	github.com/spf13/afero v1.1.2 // indirect
	github.com/spf13/cast v1.3.0 // indirect
	github.com/spf13/jwalterweatherman v1.0.0 // indirect
	github.com/subosito/gotenv v1.2.0 // indirect
	github.com/yudai/golcs v0.0.0-20170316035057-ecda9a501e82 // indirect
	golang.org/x/sys v0.0.0-20211019181941-9d821ace8654 // indirect
	golang.org/x/term v0.0.0-20210503060354-a79de5458b56 // indirect
	golang.org/x/xerrors v0.0.0-20200804184101-5ec99f83aff1 // indirect
	google.golang.org/protobuf v1.27.1 // indirect
	gopkg.in/ini.v1 v1.51.0 // indirect
	gopkg.in/yaml.v3 v3.0.0-20200313102051-9f266ea9e77c // indirect
)

replace (
	// Merging google/gopacket into akitasoftware/gopacket does not
	// bring along any tags, such as the v1.1.19 release.
	github.com/google/gopacket v1.1.19 => github.com/akitasoftware/gopacket v1.1.18-0.20210730205736-879e93dac35b
	github.com/google/martian/v3 v3.0.1 => github.com/akitasoftware/martian/v3 v3.0.1-0.20210608174341-829c1134e9de
)
