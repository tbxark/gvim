package palette

// Owner identifies the workspace model generation that started asynchronous palette
// work. A daemon hosts several workspace models in one program; their sequence numbers
// and terminal tab ids are model-local, so a message that outlives its model must not be
// applied by the newly shown model.
type Owner struct {
	// Workspace is the workspace's durable stable id (empty for a direct launch).
	Workspace string
	// Generation is unique for every built model, including a re-created workspace with
	// the same stable id after eviction.
	Generation uint64
}

// OwnedMsg is implemented by every palette message that carries asynchronous work or an
// open action across a workspace switch. The root drops an owned message whose owner is
// not the active model before any overlay sees it.
type OwnedMsg interface {
	PaletteOwner() Owner
}
