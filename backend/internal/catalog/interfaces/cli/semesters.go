// Package cli exposes trusted local operations only; it registers no HTTP routes.
package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/catalog/application"
	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/catalog/domain"
)

const Usage = `Trusted semester calendar maintenance (DATABASE_URL required except for help/validation).
  semester-maintenance list
  semester-maintenance create --file manifest.json --dry-run
  semester-maintenance create --file manifest.json --approve [--acknowledge-overlap]
  semester-maintenance inspect-impact --id UUID --starts-at RFC3339 --ends-at RFC3339 --time-zone IANA
  semester-maintenance rollover-check --from UUID --to UUID
Creation requires source, operator and approval in the reviewed JSON manifest.
Impact and rollover are read-only. Date editing/deletion/carryover are unsupported.
Exit codes: 0 success, 1 operational failure, 2 invalid input, 3 calendar conflict/not found.
`

type Open func(context.Context) (*application.CalendarService, func(), error)

func Run(ctx context.Context, args []string, out, errOut io.Writer, open Open) int {
	if len(args) == 0 || (len(args) == 1 && (args[0] == "--help" || args[0] == "help" || args[0] == "-h")) {
		fmt.Fprint(out, Usage)
		return 0
	}
	f := flag.NewFlagSet(args[0], flag.ContinueOnError)
	f.SetOutput(io.Discard)
	var file, id, start, end, zone, from, to string
	var dry, approve, overlap bool
	switch args[0] {
	case "list":
	case "create":
		f.StringVar(&file, "file", "", "reviewed JSON manifest")
		f.BoolVar(&dry, "dry-run", false, "")
		f.BoolVar(&approve, "approve", false, "")
		f.BoolVar(&overlap, "acknowledge-overlap", false, "")
	case "inspect-impact":
		f.StringVar(&id, "id", "", "")
		f.StringVar(&start, "starts-at", "", "")
		f.StringVar(&end, "ends-at", "", "")
		f.StringVar(&zone, "time-zone", "", "")
	case "rollover-check":
		f.StringVar(&from, "from", "", "")
		f.StringVar(&to, "to", "", "")
	default:
		fmt.Fprintln(errOut, "Unsupported operation. Use --help. Date edits are not supported.")
		return 2
	}
	// flag accepts repeated options by default; reject ambiguous operator intent.
	seen := map[string]bool{}
	for _, a := range args[1:] {
		if strings.HasPrefix(a, "-") {
			key := strings.SplitN(strings.TrimLeft(a, "-"), "=", 2)[0]
			if seen[key] {
				fmt.Fprintln(errOut, "Repeated flags are invalid.")
				return 2
			}
			seen[key] = true
		}
	}
	if err := f.Parse(args[1:]); errors.Is(err, flag.ErrHelp) {
		fmt.Fprint(out, Usage)
		return 0
	} else if err != nil || f.NArg() != 0 {
		fmt.Fprintln(errOut, "Invalid arguments. Use --help.")
		return 2
	}
	var input domain.CalendarInput
	var validation error
	switch args[0] {
	case "create":
		if file == "" || dry == approve || (dry && overlap) {
			validation = domain.ErrCalendarInput
			break
		}
		input, validation = readManifest(file)
		if validation == nil {
			_, validation = input.Validate()
		}
	case "inspect-impact":
		if !domain.ValidSemesterID(id) {
			validation = domain.ErrCalendarInput
			break
		}
		_, _, validation = domain.ParseSemesterBounds(start, end, zone)
	case "rollover-check":
		if !domain.ValidSemesterID(from) || !domain.ValidSemesterID(to) || strings.EqualFold(from, to) {
			validation = domain.ErrCalendarInput
		}
	}
	if validation != nil {
		fmt.Fprintln(errOut, "Invalid input: use a strict JSON manifest with ID, name, offset timestamps, matching IANA zone, HTTPS source, operator and approval; see --help.")
		return 2
	}
	service, close, err := open(ctx)
	if err != nil {
		fmt.Fprintln(errOut, "Cannot connect to PostgreSQL. Check DATABASE_URL and database availability.")
		return 1
	}
	defer close()
	var receipt application.CalendarReceipt
	switch args[0] {
	case "list":
		receipt, err = service.List(ctx)
	case "create":
		receipt, err = service.Create(ctx, input, dry, overlap)
	case "inspect-impact":
		receipt, err = service.InspectImpact(ctx, id, start, end, zone)
	case "rollover-check":
		receipt, err = service.Rollover(ctx, from, to)
	}
	if err != nil {
		if errors.Is(err, domain.ErrCalendarInput) {
			fmt.Fprintln(errOut, "Invalid calendar input.")
			return 2
		}
		for _, known := range []error{domain.ErrCalendarConflict, domain.ErrCalendarOverlap, domain.ErrCalendarMissing} {
			if errors.Is(err, known) {
				fmt.Fprintln(errOut, known)
				return 3
			}
		}
		fmt.Fprintln(errOut, "Database operation failed or timed out. No partial transaction is applied; after checking connectivity, retry the identical manifest to resolve an uncertain commit.")
		return 1
	}
	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(receipt); err != nil {
		fmt.Fprintln(errOut, "Cannot write receipt. A creation may have committed; retry the identical manifest.")
		return 1
	}
	return 0
}

func readManifest(path string) (domain.CalendarInput, error) {
	var input domain.CalendarInput
	f, err := os.Open(path)
	if err != nil {
		return input, domain.ErrCalendarInput
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, 16385))
	if err != nil || len(data) > 16384 || !utf8.Valid(data) {
		return input, domain.ErrCalendarInput
	}
	// Detect duplicate JSON keys rather than allowing the decoder's last value to win.
	d := json.NewDecoder(strings.NewReader(string(data)))
	token, err := d.Token()
	if err != nil || token != json.Delim('{') {
		return input, domain.ErrCalendarInput
	}
	seen := map[string]bool{}
	for d.More() {
		key, err := d.Token()
		if err != nil {
			return input, domain.ErrCalendarInput
		}
		k, ok := key.(string)
		if !ok || seen[strings.ToLower(k)] {
			return input, domain.ErrCalendarInput
		}
		seen[strings.ToLower(k)] = true
		var value string
		if err := d.Decode(&value); err != nil {
			return input, domain.ErrCalendarInput
		}
	}
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		return input, domain.ErrCalendarInput
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return input, domain.ErrCalendarInput
	}
	return input, nil
}
