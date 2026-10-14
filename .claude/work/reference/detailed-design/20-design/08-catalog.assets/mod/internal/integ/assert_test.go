package integ

import (
	"github.com/cantara/source-code-portal/internal/catalog"
	"github.com/cantara/source-code-portal/internal/httpapi"
	"github.com/cantara/source-code-portal/internal/reactor"
	"github.com/cantara/source-code-portal/internal/readmodel"
)

// the compile-time assertions of design section 2.4
var (
	_ catalog.EventSink = (*reactor.Fan)(nil)
	_ catalog.Reader    = (*readmodel.Publisher)(nil)
	_ httpapi.Mounter   = (*catalog.Service)(nil)
	_ reactor.Func      = (*catalog.Service)(nil).React
)
