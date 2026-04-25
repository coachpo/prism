package runtime_test

import "testing"

func BenchmarkRuntimeHotPathNoDBCoordination(b *testing.B) {
	BenchmarkRuntimeHotPathBaseline(b)
}

func BenchmarkRuntimeAdmissionContention(b *testing.B) {
	BenchmarkRuntimeLocalAdmissionContention(b)
}

func BenchmarkRuntimeRoundRobinContention(b *testing.B) {
	BenchmarkRuntimeLocalRoundRobinContention(b)
}

func BenchmarkRuntimeRuntimeVsManagementLoad(b *testing.B) {
	BenchmarkRuntimeVsManagementLoad(b)
}
