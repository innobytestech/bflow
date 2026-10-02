# Impl GH-50
- R1/R3/R4/R5/R6: TestMeasurePending, TestIsGenerated, TestExempt (internal/review) cubren Pending, exentos y binarios.
- R7: TestFromActionGitDeleted (rutas tras `--` aunque esten borradas); TestReviewerApprovedNeedsRedRead (engine) rechaza con c.go sin abrir.
- R9/R10/R12: TestReviewerRejectedNotBlocked, TestReviewCoverageLogged (ahora via REJECTED), pending en review_coverage.
- R13: TestWalkthroughShowsCoverage/medida (4 de 4).
- Pruebas congeladas ajustadas con `freeze --allow` aprobado: binary img/logo.png en Measure (5 pendientes con diff entero), RedRead lee tambien c.go, CoverageLogged usa REJECTED, walkthrough lee c.go.
- Mutaciones: no repetidas en esta pasada; la logica de Measure no cambio, solo se alinearon las pruebas.
- bflow check GH-50 en verde.
