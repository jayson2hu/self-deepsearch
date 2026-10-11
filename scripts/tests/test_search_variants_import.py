"""The live integration script must remain safe for default pytest discovery."""
import runpy
from pathlib import Path
from types import SimpleNamespace
from unittest.mock import Mock, patch


def test_discovery_does_not_query_database_or_overwrite_evidence(tmp_path, monkeypatch, capsys):
    script = Path(__file__).resolve().parents[1] / "test_search_variants.py"
    read_people = Mock(side_effect=AssertionError("discovery must never query a live database"))
    monkeypatch.chdir(tmp_path)
    with patch.dict("sys.modules", {"collector_integration": SimpleNamespace(read_people=read_people)}):
        module = runpy.run_path(str(script))
    assert callable(module["main"])
    read_people.assert_not_called()
    assert list(tmp_path.iterdir()) == []
    assert capsys.readouterr().out == ""
