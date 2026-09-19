package provider

// Capabilities 是上游能提供哪些字段/能力的位集，用于驱动 UI 的列显示策略。
type Capabilities uint32

const (
	CapSecondaryTypes Capabilities = 1 << iota
	CapComposer
	CapLyricist
	CapPerformerRoles
	CapLabel
	CapCatalogNumber
	CapMediumFormat
	CapCountry
	CapISRC
	CapCoverArt
	CapMultiSizeCover
	// CapUpstreamScore 表示搜索响应带相关度分值。
	CapUpstreamScore
	// CapPrimaryType 表示提供主要类型。
	CapPrimaryType
)

// Has 报告能力集是否包含某项能力。
func (c Capabilities) Has(cap Capabilities) bool { return c&cap != 0 }

// capNames 是各能力位的稳定标识，供 UI 的列策略与设置页对照使用。
var capNames = []struct {
	Cap  Capabilities
	Name string
}{
	{CapSecondaryTypes, "SecondaryTypes"},
	{CapComposer, "Composer"},
	{CapLyricist, "Lyricist"},
	{CapPerformerRoles, "PerformerRoles"},
	{CapLabel, "Label"},
	{CapCatalogNumber, "CatalogNumber"},
	{CapMediumFormat, "MediumFormat"},
	{CapCountry, "Country"},
	{CapISRC, "ISRC"},
	{CapCoverArt, "CoverArt"},
	{CapMultiSizeCover, "MultiSizeCover"},
	{CapUpstreamScore, "UpstreamScore"},
	{CapPrimaryType, "PrimaryType"},
}

// Names 返回能力集中所有被置位的能力名，供前端渲染列策略。
func (c Capabilities) Names() []string {
	var out []string
	for _, entry := range capNames {
		if c.Has(entry.Cap) {
			out = append(out, entry.Name)
		}
	}
	return out
}
