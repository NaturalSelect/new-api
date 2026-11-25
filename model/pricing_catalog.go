package model

import "github.com/QuantumNous/new-api/pkg/modelcatalog"

func applyCatalogMetadata(p *Pricing, e *modelcatalog.Entry) {
	p.ContextLength = e.ContextLength
	p.MaxOutputTokens = e.MaxCompletionTokens
	p.InputModalities, p.OutputModalities = e.FrontendModalities()
	p.KnowledgeCutoff = e.KnowledgeCutoff
	p.ReleaseDate = e.ReleaseDate()
	p.Capabilities = e.Capabilities()
}
