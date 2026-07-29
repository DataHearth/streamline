package api

import (
	"net/http"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("REST API system", Label("e2e"), func() {
	It("returns the runtime environment summary", func() {
		resp := get("/api/v1/system/info", adminAuth)
		defer resp.Body.Close()
		Expect(resp.StatusCode).To(Equal(http.StatusOK))
		var info struct {
			AppName   string `json:"app_name"`
			AuthMode  string `json:"auth_mode"`
			DataDir   string `json:"data_dir"`
			DbPath    string `json:"db_path"`
			GoVersion string `json:"go_version"`
			GoOsArch  string `json:"go_os_arch"`
			Version   string `json:"version"`
		}
		decode(resp, &info)
		Expect(info.AppName).NotTo(BeEmpty())
		Expect(info.AuthMode).To(Equal("full"))
		Expect(info.DataDir).NotTo(BeEmpty())
		Expect(info.DbPath).NotTo(BeEmpty())
		Expect(info.GoVersion).To(HavePrefix("go"))
		Expect(info.GoOsArch).To(ContainSubstring("/"))
	})

	It("403s for a non-admin", func() {
		resp := get("/api/v1/system/info", viewerAuth)
		defer resp.Body.Close()
		Expect(resp.StatusCode).To(Equal(http.StatusForbidden))
	})
})
