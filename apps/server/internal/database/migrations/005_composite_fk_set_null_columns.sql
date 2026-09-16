-- 复合外键 ON DELETE SET NULL 必须带列清单：不带清单时 PostgreSQL 把全部外键列
-- （含 NOT NULL 的 user_id）一起置 NULL。注销清除（DELETE /v1/me/data）删被引用行时，
-- 引用方行还在 → 23502，整个注销事务失败（9.16 生产事故：DELETE FROM tasks 经
-- tasks→provider_invocations→media_assets 级联，在 hair_previews 上炸开）。
-- 列清单语义不变：仍只把引用列置空，user_id 不再被波及。

ALTER TABLE diagnostics
  DROP CONSTRAINT diagnostics_user_id_source_media_asset_id_fkey,
  ADD CONSTRAINT diagnostics_user_id_source_media_asset_id_fkey
    FOREIGN KEY (user_id, source_media_asset_id)
    REFERENCES media_assets(user_id, id) ON DELETE SET NULL (source_media_asset_id);

ALTER TABLE hair_previews
  DROP CONSTRAINT hair_previews_user_id_source_media_asset_id_fkey,
  ADD CONSTRAINT hair_previews_user_id_source_media_asset_id_fkey
    FOREIGN KEY (user_id, source_media_asset_id)
    REFERENCES media_assets(user_id, id) ON DELETE SET NULL (source_media_asset_id);

ALTER TABLE hair_previews
  DROP CONSTRAINT hair_previews_user_id_render_publication_id_fkey,
  ADD CONSTRAINT hair_previews_user_id_render_publication_id_fkey
    FOREIGN KEY (user_id, render_publication_id)
    REFERENCES render_publications(user_id, id) ON DELETE SET NULL (render_publication_id);

ALTER TABLE hair_previews
  DROP CONSTRAINT hair_previews_result_media_fk,
  ADD CONSTRAINT hair_previews_result_media_fk
    FOREIGN KEY (user_id, result_media_asset_id)
    REFERENCES media_assets(user_id, id) ON DELETE SET NULL (result_media_asset_id);

ALTER TABLE today_plans
  DROP CONSTRAINT today_plans_user_id_report_id_fkey,
  ADD CONSTRAINT today_plans_user_id_report_id_fkey
    FOREIGN KEY (user_id, report_id)
    REFERENCES reports(user_id, id) ON DELETE SET NULL (report_id);

ALTER TABLE today_plans
  DROP CONSTRAINT today_plans_user_id_render_publication_id_fkey,
  ADD CONSTRAINT today_plans_user_id_render_publication_id_fkey
    FOREIGN KEY (user_id, render_publication_id)
    REFERENCES render_publications(user_id, id) ON DELETE SET NULL (render_publication_id);

ALTER TABLE wardrobe_items
  DROP CONSTRAINT wardrobe_items_user_id_media_asset_id_fkey,
  ADD CONSTRAINT wardrobe_items_user_id_media_asset_id_fkey
    FOREIGN KEY (user_id, media_asset_id)
    REFERENCES media_assets(user_id, id) ON DELETE SET NULL (media_asset_id);

ALTER TABLE wardrobe_outfits
  DROP CONSTRAINT wardrobe_outfits_user_id_selected_plan_id_fkey,
  ADD CONSTRAINT wardrobe_outfits_user_id_selected_plan_id_fkey
    FOREIGN KEY (user_id, selected_plan_id)
    REFERENCES plan_variants(user_id, id) ON DELETE SET NULL (selected_plan_id);
