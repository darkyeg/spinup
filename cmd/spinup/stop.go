package main

// stopService ends a running service, which hands the accounts on first, before the autostart step restarts or removes it.
func stopService() {
	service, err := keyedLocalService()
	if err != nil {
		return
	}
	if _, err := service.leader(); err != nil {
		return
	}
	step("Stopping the running service")
	if err := service.stop(); err != nil {
		step("The service didn't stop by itself (%v); stopping it anyway", err)
	}
}
