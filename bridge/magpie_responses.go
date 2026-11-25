package main

import "errors"

type responsesFormat string

const (
	responsesAutomatic responsesFormat = "automatic"
	responsesStandard  responsesFormat = "responses"
	responsesLite      responsesFormat = "responses-lite"
)

var responsesFormats = [...]responsesFormat{responsesAutomatic, responsesStandard, responsesLite}

var errMagpieResponsesLiteUnavailable = errors.New("Responses Lite is unavailable for the saved Magpie route; restore Lite support in Magpie")

func (f responsesFormat) label() string {
	switch f {
	case responsesStandard:
		return "Responses"
	case responsesLite:
		return "Responses Lite"
	default:
		return "Automatic"
	}
}

func (d *sessionDraft) resolveResponsesFormat(supportsLite bool) error {
	if d.ResponsesLiteModel != "" && !supportsLite {
		return errMagpieResponsesLiteUnavailable
	}
	d.ResponsesLiteModel = ""
	switch d.ResponsesFormat {
	case "", responsesAutomatic:
		if supportsLite {
			d.ResponsesFormat = responsesAutomatic
			d.ResponsesLiteModel = d.Model
		}
	case responsesStandard:
	case responsesLite:
		if !supportsLite {
			return errMagpieResponsesLiteUnavailable
		}
		d.ResponsesLiteModel = d.Model
	default:
		return errors.New("invalid Responses format; choose Automatic, Responses or Responses Lite")
	}
	return nil
}

func (s session) responsesFormatLabel() string {
	if s.ResponsesFormat == responsesAutomatic {
		if s.ResponsesLiteModel != "" {
			return "Automatic · Responses Lite"
		}
		return "Automatic · Responses"
	}
	return s.ResponsesFormat.label()
}
