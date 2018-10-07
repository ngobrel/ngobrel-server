
CREATE TABLE chat_list (
  user_id UUID not null,
  chat_id UUID not null,
  created_at TIMESTAMP not null,
  updated_at TIMESTAMP not null,
  excerpt TEXT default '',
  is_admin INT default 0,
  PRIMARY KEY (user_id, chat_id)
);
