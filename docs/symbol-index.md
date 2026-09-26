# Named Go declaration index

[Back to handoff](README.md)

This inventory was checked against the saved Go source on September 26, 2026. Each source file links to the reference that explains its declarations. Repeated method names belong to different receiver types; for example, both stores implement `Records`. Line numbers are navigation hints for this version and can change after edits.

This lists named functions/methods and named structs/interfaces, including the local `recoveryScenario`. Anonymous payloads, test cases, callbacks, template helpers, SQL functions, and browser-test methods are explained in the accompanying references. It is an index, not a replacement for those explanations.

Coverage: **103 named functions/methods** and **17 named structs/interfaces** across 12 Go files.

## bulk_seed.go

[Source](../bulk_seed.go) | [Explanation](templates-tests-cleanup.md)

| Declaration | Source line |
| --- | --- |
| `addBulkPatients()` | 13 |
| `struct recoveryScenario` | 16 |

## bulk_seed_test.go

[Source](../bulk_seed_test.go) | [Explanation](templates-tests-cleanup.md)

| Declaration | Source line |
| --- | --- |
| `TestBulkDemoPatientsHaveIndividualCompleteCharts()` | 11 |

## directory_test.go

[Source](../directory_test.go) | [Explanation](templates-tests-cleanup.md)

| Declaration | Source line |
| --- | --- |
| `TestDoctorCanSelectFirstMiddleAndLastPatient()` | 13 |
| `TestMockScanIsPatientSpecificAndCareTeamScoped()` | 45 |
| `TestBulkDoctorReportsRemainScoped()` | 92 |

## handlers.go

[Source](../handlers.go) | [Explanation](runtime-reference.md)

| Declaration | Source line |
| --- | --- |
| `validRole()` | 20 |
| `validType()` | 21 |
| `(*app).setSession()` | 25 |
| `(*app).sessionFor()` | 40 |
| `(*app).home()` | 51 |
| `(*app).demo()` | 59 |
| `(*app).dashboardData()` | 82 |
| `plainSummary()` | 143 |
| `(*app).dashboard()` | 156 |
| `(*app).readError()` | 184 |
| `sameOrigin()` | 196 |
| `(*app).authorizePost()` | 207 |
| `parseForm()` | 220 |
| `(*app).addRecord()` | 225 |
| `(*app).uploadReport()` | 275 |
| `(*app).addHealing()` | 338 |
| `(*app).onboardForm()` | 379 |
| `(*app).onboard()` | 390 |
| `feedbackHTML()` | 439 |
| `(*app).feedback()` | 447 |
| `(*app).writeCounts()` | 454 |

## live.go

[Source](../live.go) | [Explanation](runtime-reference.md)

| Declaration | Source line |
| --- | --- |
| `struct change` | 12 |
| `struct eventHub` | 14 |
| `newEventHub()` | 20 |
| `(*eventHub).subscribe()` | 23 |
| `(*eventHub).publish()` | 30 |
| `(*eventHub).close()` | 40 |
| `startSSE()` | 42 |
| `patch()` | 50 |
| `writeStream()` | 63 |
| `(*app).events()` | 76 |
| `(*app).doctorContext()` | 166 |
| `(*app).snapshot()` | 179 |

## live_test.go

[Source](../live_test.go) | [Explanation](templates-tests-cleanup.md)

| Declaration | Source line |
| --- | --- |
| `openTestStream()` | 15 |
| `readTestSnapshot()` | 41 |
| `timelineEvent()` | 60 |
| `TestLiveDoctorStreamReceivesPatientUpload()` | 69 |
| `TestLivePatientStreamPreservesFilterAndPatientScope()` | 89 |
| `struct deadlineRecorder` | 115 |
| `(*deadlineRecorder).SetWriteDeadline()` | 122 |
| `TestStreamWritesClearDeadlineForIdleInterval()` | 127 |

## main.go

[Source](../main.go) | [Explanation](runtime-reference.md)

| Declaration | Source line |
| --- | --- |
| `struct session` | 22 |
| `struct app` | 27 |
| `newApp()` | 38 |
| `(*app).routes()` | 68 |
| `(*app).render()` | 97 |
| `(*app).page()` | 103 |
| `main()` | 114 |

## main_test.go

[Source](../main_test.go) | [Explanation](templates-tests-cleanup.md)

| Declaration | Source line |
| --- | --- |
| `testApp()` | 18 |
| `demoSession()` | 28 |
| `serveRequest()` | 50 |
| `postForm()` | 59 |
| `assertSSE()` | 65 |
| `TestDashboardsRenderAndDemoSessionsCoexist()` | 77 |
| `TestDoctorRecordEscapedAndVisibleToPatient()` | 128 |
| `TestRejectedRecordsDoNotWrite()` | 158 |
| `uploadRequest()` | 202 |
| `TestUploadStoresFilenameAndSharesWithCareTeam()` | 228 |
| `TestRejectedUploadsDoNotWrite()` | 252 |
| `TestOnboardingCreatesRoleSpecificProfiles()` | 289 |
| `TestLivePatientStreamReceivesDoctorRecord()` | 353 |

## mock_scans.go

[Source](../mock_scans.go) | [Explanation](templates-tests-cleanup.md)

| Declaration | Source line |
| --- | --- |
| `(*app).mockScan()` | 12 |

## models.go

[Source](../models.go) | [Explanation](storage-database.md)

| Declaration | Source line |
| --- | --- |
| `struct Profile` | 13 |
| `struct Record` | 17 |
| `struct Upload` | 22 |
| `struct Healing` | 27 |
| `struct Biometric` | 33 |
| `struct Dashboard` | 40 |
| `interface Store` | 52 |
| `newID()` | 72 |

## selection_test.go

[Source](../selection_test.go) | [Explanation](templates-tests-cleanup.md)

| Declaration | Source line |
| --- | --- |
| `TestDoctorSignalSelectionPatchesOnlyTheSelectedChart()` | 11 |
| `TestDoctorSignalSelectionValidatesCareTeamAndJSON()` | 54 |

## store.go

[Source](../store.go) | [Explanation](storage-database.md)

| Declaration | Source line |
| --- | --- |
| `struct memoryStore` | 20 |
| `NewMemoryStore()` | 34 |
| `struct seedRecord` | 51 |
| `(*memoryStore).Close()` | 96 |
| `(*memoryStore).Profile()` | 97 |
| `(*memoryStore).Profiles()` | 109 |
| `(*memoryStore).CareTeam()` | 124 |
| `(*memoryStore).Patients()` | 137 |
| `(*memoryStore).Records()` | 152 |
| `(*memoryStore).Uploads()` | 167 |
| `(*memoryStore).Reports()` | 185 |
| `(*memoryStore).Healing()` | 201 |
| `(*memoryStore).Biometrics()` | 223 |
| `(*memoryStore).IsCareTeam()` | 235 |
| `(*memoryStore).CreateProfile()` | 243 |
| `(*memoryStore).AddRecord()` | 266 |
| `(*memoryStore).AddUpload()` | 285 |
| `(*memoryStore).AddHealing()` | 301 |
| `struct postgresStore` | 318 |
| `NewPostgresStore()` | 322 |
| `(*postgresStore).Close()` | 336 |
| `storageError()` | 337 |
| `interface rowScanner` | 346 |
| `scanProfile()` | 352 |
| `(*postgresStore).Profile()` | 357 |
| `(*postgresStore).queryProfiles()` | 360 |
| `(*postgresStore).Profiles()` | 376 |
| `(*postgresStore).CareTeam()` | 379 |
| `(*postgresStore).Patients()` | 382 |
| `scanRecord()` | 385 |
| `(*postgresStore).Records()` | 393 |
| `scanUpload()` | 409 |
| `(*postgresStore).Uploads()` | 414 |
| `(*postgresStore).Reports()` | 430 |
| `scanHealing()` | 450 |
| `(*postgresStore).Healing()` | 455 |
| `(*postgresStore).Biometrics()` | 471 |
| `(*postgresStore).IsCareTeam()` | 476 |
| `(*postgresStore).CreateProfile()` | 481 |
| `(*postgresStore).AddRecord()` | 507 |
| `(*postgresStore).AddUpload()` | 519 |
| `(*postgresStore).AddHealing()` | 524 |
