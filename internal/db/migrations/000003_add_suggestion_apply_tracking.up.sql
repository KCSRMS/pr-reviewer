ALTER TABLE public.review_comments
    ADD COLUMN applied_at timestamp with time zone,
    ADD COLUMN applied_by text DEFAULT ''::text NOT NULL;
