package util_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/dcm-project/cnpg-database-service-provider/internal/util"
)

var _ = Describe("Ptr", func() {
	It("returns a pointer to a string value", func() {
		p := util.Ptr("hello")
		Expect(p).NotTo(BeNil())
		Expect(*p).To(Equal("hello"))
	})

	It("returns a pointer to an int value", func() {
		p := util.Ptr(42)
		Expect(p).NotTo(BeNil())
		Expect(*p).To(Equal(42))
	})

	It("returns a pointer to an int32 value", func() {
		p := util.Ptr(int32(400))
		Expect(p).NotTo(BeNil())
		Expect(*p).To(Equal(int32(400)))
	})
})
