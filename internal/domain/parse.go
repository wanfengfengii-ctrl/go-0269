package domain

// ParseResourceType maps a wire resource type name to its enum value.
func ParseResourceType(s string) (ResourceType, bool) {
	switch s {
	case "INCUBATION_CABIN":
		return ResourceIncubationCabin, true
	case "COLD_STORAGE_CELL":
		return ResourceColdStorageCell, true
	case "SLIDE":
		return ResourceSlide, true
	default:
		return 0, false
	}
}

// ParseInstrumentType maps a wire instrument type name to its enum value.
func ParseInstrumentType(s string) (InstrumentType, bool) {
	switch s {
	case "MICROSCOPE":
		return InstrumentMicroscope, true
	case "TEMP_HUMIDITY_PROBE":
		return InstrumentTempHumidityProbe, true
	case "MOISTURE_METER":
		return InstrumentMoistureMeter, true
	default:
		return 0, false
	}
}

// ParseEvidenceType maps a wire evidence type name to its enum value.
func ParseEvidenceType(s string) (EvidenceType, bool) {
	switch s {
	case "MICROSCOPY":
		return EvidenceMicroscopy, true
	case "ENVIRONMENT":
		return EvidenceEnvironment, true
	case "MOISTURE":
		return EvidenceMoisture, true
	case "DAMAGE":
		return EvidenceDamage, true
	default:
		return 0, false
	}
}

// ParseFinalDecisionType maps a wire decision name to its enum value.
func ParseFinalDecisionType(s string) (FinalDecisionType, bool) {
	switch s {
	case "admit":
		return FinalAdmit, true
	case "isolate":
		return FinalIsolate, true
	case "cancel":
		return FinalCancel, true
	default:
		return 0, false
	}
}
