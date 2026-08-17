package web

import (
	svcmodsec "github.com/lyracorp/xmanager/internal/services/modsecurity"
)

type modsecurityService = svcmodsec.Service

func newModsecurityService(h *handler) *modsecurityService {
	return svcmodsec.New(h.opts.DB, h.localServerID(), h.reloadSecurityAll)
}

func (h *handler) lookupModsecurityService() *modsecurityService {
	return newModsecurityService(h)
}
