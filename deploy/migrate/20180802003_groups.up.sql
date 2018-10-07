
CREATE TABLE group_list (
  chat_id UUID not null,
  creator_id UUID not null,
  created_at TIMESTAMP not null,
  updated_at TIMESTAMP not null,
  title TEXT default '',
  avatar BYTEA null,
  PRIMARY KEY (chat_id)
);

CREATE INDEX group_list_creator_id on group_list(creator_id);
