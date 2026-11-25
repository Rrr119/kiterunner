package cmd

import (
	"fmt"
	"os"

	"github.com/assetnote/kiterunner/pkg/log"
	"github.com/assetnote/kiterunner/pkg/openapi"
	"github.com/spf13/cobra"
)

var (
	openapiManualRoutes = ""
	openapiReportJSON   = false
	openapiReportOutput = ""
)

// openapiReportCmd parses one or more specs and reports how completely they
// were translated into scan-ready routes.
var openapiReportCmd = &cobra.Command{
	Use:     "openapi-report SPEC [SPEC...]",
	Aliases: []string{"openapi"},
	Short:   "parse OpenAPI/Swagger specs and report route coverage",
	Long: `parse OpenAPI/Swagger specs (JSON or YAML) and report how many spec
operations were translated into kiterunner routes.

The generated routes use the same proute representation as .kite wordlists, so
they can be scanned directly or compiled into a .kite file.

--manual accepts a text file containing the manually enumerated routes, one
per line, e.g.:

  GET /v2/pet/{petId}
  POST /v2/pet

The manual comparison is useful for regression testing spec coverage.`,
	Args: cobra.MinimumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		if openapiManualRoutes != "" && len(args) != 1 {
			log.Fatal().Msg("--manual can only be used with one spec; run the command once per spec")
		}

		reports := make([]openapi.CoverageReport, 0, len(args))
		for _, filename := range args {
			doc, err := openapi.LoadFile(filename)
			if err != nil {
				log.Fatal().Err(err).Str("spec", filename).Msg("failed to parse OpenAPI spec")
			}

			report := doc.CoverageReport()
			if openapiManualRoutes != "" {
				manual, err := openapi.LoadManualRoutes(openapiManualRoutes)
				if err != nil {
					log.Fatal().Err(err).Str("manual", openapiManualRoutes).Msg("failed to load manual routes")
				}
				report = doc.CoverageReportWithManual(manual)
			}
			reports = append(reports, report)
		}

		format := "text"
		if openapiReportJSON {
			format = "json"
		}
		if openapiReportOutput != "" {
			if err := openapi.WriteReportFile(openapiReportOutput, reports, format); err != nil {
				log.Fatal().Err(err).Msg("failed to write report")
			}
			fmt.Fprintf(os.Stderr, "wrote %d coverage report(s) to %s\n", len(reports), openapiReportOutput)
			return
		}
		if err := openapi.WriteReports(os.Stdout, reports, format); err != nil {
			log.Fatal().Err(err).Msg("failed to write report")
		}
	},
}

func init() {
	kidebuilderCmd.AddCommand(openapiReportCmd)

	openapiReportCmd.Flags().StringVar(&openapiManualRoutes, "manual", openapiManualRoutes, "manually enumerated routes to compare against (METHOD /path per line)")
	openapiReportCmd.Flags().BoolVar(&openapiReportJSON, "json", openapiReportJSON, "write the report as JSON")
	openapiReportCmd.Flags().StringVarP(&openapiReportOutput, "output", "o", openapiReportOutput, "write the report to this file instead of stdout")
}
