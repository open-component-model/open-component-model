# Readme

The Helm chart that is used for testing is copied from [Helm chart museum repository](https://github.com/helm/chartmuseum/tree/main/testdata/charts/mychart)

Three chart formats are tested:

* Non-packaged (directory)
* Unsigned packaged chart (tgz)
* Signed packaged chart (tgz + tgz.prov)

The `badchart` is an invalid chart and is used to test that this is detected by chart validation.
