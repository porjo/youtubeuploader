#!/bin/bash

TAG=$1

[ $# -ne 1 ] && echo "$0: git tag missing" && exit 1

! git verify-tag $TAG &> /dev/null && echo "ERROR: git tag $TAG not signed" && exit 1

exit 0
