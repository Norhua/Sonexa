package store

const taskChangeSchema = `
CREATE TABLE task_change_state(id INTEGER PRIMARY KEY CHECK(id=1),revision INTEGER NOT NULL);
INSERT INTO task_change_state VALUES(1,1);
`

func taskChangeTriggers() string {
	schema := taskChangeSchema
	for _, table := range []string{"import_item", "import_batch", "review_problem", "review_problem_item"} {
		for _, event := range []string{"INSERT", "UPDATE", "DELETE"} {
			schema += "CREATE TRIGGER task_changed_" + table + "_" + event + " AFTER " + event + " ON " + table + " BEGIN UPDATE task_change_state SET revision=revision+1 WHERE id=1; END;\n"
		}
	}
	return schema
}
