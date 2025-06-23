package ddl

func closeQuietly(closer func() error) {
	if closer != nil {
		_ = closer()
	}
}
