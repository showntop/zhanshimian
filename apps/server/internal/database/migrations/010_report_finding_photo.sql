-- Findings record which analysis photo they were observed on, so the client can
-- render anchors only on the matching hero image (report hero = body photo).
-- Legacy rows were only ever painted on the body photo, hence the default.
ALTER TABLE report_findings
  ADD COLUMN photo text NOT NULL DEFAULT 'body';
