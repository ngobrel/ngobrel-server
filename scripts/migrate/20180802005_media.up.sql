
CREATE TABLE media (
  uploader UUID not null,
  file_id TEXT not null,
  created_at TIMESTAMP not null,
  is_encrypted BOOLEAN,
  file_name TEXT,
  content_type TEXT,
  file_size BIGINT,

  PRIMARY KEY (uploader, file_id)
);
