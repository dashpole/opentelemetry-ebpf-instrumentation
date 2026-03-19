headers = """# OBI Memory Investigation Results

| Protocol | Config | QPS | In-Use Memory | Total Allocated Memory | Spans | Metrics (Datapoints / Cardinality) | Artifacts |
| :--- | :--- | :--- | :--- | :--- | :--- | :--- | :--- |
"""
with open('temp_results.md') as f1, open('results.md', 'w') as f2:
    f2.write(headers + f1.read())
