package buildinfo

const (
	Product                 = "arbiter"
	Version                 = "1.4.0"
	OperatorContractVersion = "operator.v1"
)

type OperatorInfo struct {
	Product                 string `json:"product"`
	BuildVersion            string `json:"build_version"`
	OperatorContractVersion string `json:"operator_contract_version"`
}

func Current() OperatorInfo {
	return OperatorInfo{
		Product:                 Product,
		BuildVersion:            Version,
		OperatorContractVersion: OperatorContractVersion,
	}
}
