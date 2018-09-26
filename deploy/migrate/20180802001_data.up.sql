INSERT INTO "devices" ("user_id", "device_id", "created_at", "updated_at", "device_state") VALUES ('10000000-0000-0000-0000-000000000001', '10000000-0000-0000-1000-000000000001', now(), now(), '1');
INSERT INTO "devices" ("user_id", "device_id", "created_at", "updated_at", "device_state") VALUES ('10000000-0000-0000-0000-000000000002', '10000000-0000-0000-1000-000000000002', now(), now(), '1');
INSERT INTO "chat_list" ("user_id", "chat_id", "created_at", "updated_at") VALUES ('10000000-0000-0000-0000-000000000001', '10000000-0000-0000-0000-000000000002', now(), now());
INSERT INTO "chat_list" ("user_id", "chat_id", "created_at", "updated_at") VALUES ('10000000-0000-0000-0000-000000000001', '10000000-0000-0000-0000-000000000003', now(), now());
INSERT INTO "chat_list" ("user_id", "chat_id", "created_at", "updated_at") VALUES ('10000000-0000-0000-0000-000000000001', '10000000-0000-0000-0000-000000000004', now(), now());
INSERT INTO "chat_list" ("user_id", "chat_id", "created_at", "updated_at") VALUES ('10000000-0000-0000-0000-000000000002', '10000000-0000-0000-0000-000000000001', now(), now());

INSERT INTO "contacts" ("user_id", "chat_id", "chat_type", "name", "created_at", "updated_at", "notification") VALUES ('10000000-0000-0000-0000-000000000001', '10000000-0000-0000-0000-000000000002', 0, 'Rudi Hartono', now(), now(), 0);
INSERT INTO "contacts" ("user_id", "chat_id", "chat_type", "name", "created_at", "updated_at", "notification") VALUES ('10000000-0000-0000-0000-000000000001', '10000000-0000-0000-0000-000000000003', 0, 'Rustam Herlambang', now(), now(), 100);
INSERT INTO "contacts" ("user_id", "chat_id", "chat_type", "name", "created_at", "updated_at", "notification") VALUES ('10000000-0000-0000-0000-000000000001', '10000000-0000-0000-0000-000000000004', 0, 'Rahmat Kartolo', now(), now(), 0);
INSERT INTO "contacts" ("user_id", "chat_id", "chat_type", "name", "created_at", "updated_at", "notification") VALUES ('10000000-0000-0000-0000-000000000003', '10000000-0000-0000-0000-000000000002', 0, 'Rudi Hartono', now(), now(), 0);
