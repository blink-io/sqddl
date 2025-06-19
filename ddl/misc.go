package ddl

func closeQuietly(closer func() error) {
	_ = closer()
}
