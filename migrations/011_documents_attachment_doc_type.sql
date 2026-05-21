-- Allow doc_type = 'attachment' (supplementary documents, max 5 per project in app).

ALTER TABLE documents DROP CONSTRAINT IF EXISTS documents_doc_type_check;

ALTER TABLE documents ADD CONSTRAINT documents_doc_type_check
  CHECK (doc_type IN (
    'quotation_support',
    'quotation_dealer',
    'tor_support',
    'tor_dealer',
    'contract',
    'closing',
    'site_survey',
    'attachment'
  ));
