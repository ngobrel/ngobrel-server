
CREATE TABLE contacts (
  user_id UUID not null,
  chat_id UUID not null,
  chat_type SMALLINT not null,
  name text,
  created_at TIMESTAMP not null,
  updated_at TIMESTAMP not null,
  notification INT not null,
  PRIMARY KEY (user_id, chat_id)
);